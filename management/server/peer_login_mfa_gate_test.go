package server

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/activity"
	"github.com/openzro/openzro/management/server/auth/jwt"
	"github.com/openzro/openzro/management/server/settings"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// Switching the device-login gate on, or widening the users it applies to
// while it is on, is refused while an active user it would apply to has no
// 2FA (ADR-0028 D10). It is an activation guardrail: a settings change that
// does not widen the gate is accepted even with such users.
func TestUpdateAccountSettings_PeerLoginMFAActivationCheck(t *testing.T) {
	localUser := jwt.EncodeDexSubject("third-party", "local")

	type toggles struct{ gate, local, federated bool }
	tests := []struct {
		name string
		from toggles
		to   toggles
		// enrolled lists the users with 2FA; the owner is federated.
		enrolled []string
		refused  bool
	}{
		{"gate on, local user without 2FA", toggles{local: true}, toggles{true, true, false}, nil, true},
		{"local on with gate on", toggles{gate: true}, toggles{true, true, false}, nil, true},
		{"federated on with gate on", toggles{gate: true}, toggles{true, false, true}, []string{localUser}, true},
		{"all three in one request", toggles{}, toggles{true, true, true}, []string{"owner"}, true},
		{"gate on, federated users not subject", toggles{local: true}, toggles{true, true, false},
			[]string{localUser}, false},
		{"gate on with no enforcement", toggles{}, toggles{gate: true}, nil, false},
		{"gate already on, nothing widened", toggles{true, true, false}, toggles{true, true, false}, nil, false},
		{"gate off", toggles{true, true, false}, toggles{false, true, false}, nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			am := newMFATestManager(t)
			ctx := context.Background()
			acc, err := am.GetOrCreateAccountByUser(ctx, "owner", "example.com")
			require.NoError(t, err)
			seedMFAUser(t, am, acc.Id, localUser, types.UserRoleUser)
			for _, id := range tc.enrolled {
				seedMFARow(t, am, id, nil)
			}

			settings := acc.Settings.Copy()
			if settings.Extra == nil {
				settings.Extra = &types.ExtraSettings{}
			}
			settings.MFAEnforcePeerLogin = tc.from.gate
			settings.MFAEnforceLocal = tc.from.local
			settings.MFAEnforceFederated = tc.from.federated
			require.NoError(t, am.Store.SaveAccountSettings(ctx, store.LockingStrengthUpdate, acc.Id, settings))

			update := settings.Copy()
			update.MFAEnforcePeerLogin = tc.to.gate
			update.MFAEnforceLocal = tc.to.local
			update.MFAEnforceFederated = tc.to.federated
			_, err = am.UpdateAccountSettings(ctx, acc.Id, "owner", update)

			stored, gerr := am.Store.GetAccountSettings(ctx, store.LockingStrengthNone, acc.Id)
			require.NoError(t, gerr)
			if !tc.refused {
				require.NoError(t, err)
				assert.Equal(t, tc.to.gate, stored.MFAEnforcePeerLogin)
				return
			}
			require.Error(t, err)
			s, ok := status.FromError(err)
			require.True(t, ok, "want a status error, got %v", err)
			assert.Equal(t, status.PreconditionFailed, s.Type())
			assert.Equal(t, tc.from, toggles{stored.MFAEnforcePeerLogin, stored.MFAEnforceLocal, stored.MFAEnforceFederated},
				"a refused change leaves the settings as they were")
		})
	}
}

// Switching the device-login gate is audited with the admin who did it.
func TestUpdateAccountSettings_PeerLoginMFAAudit(t *testing.T) {
	am := newMFATestManager(t)
	ctx := context.Background()
	acc, err := am.GetOrCreateAccountByUser(ctx, "owner", "example.com")
	require.NoError(t, err)

	update := acc.Settings.Copy()
	update.MFAEnforcePeerLogin = true
	_, err = am.UpdateAccountSettings(ctx, acc.Id, "owner", update)
	require.NoError(t, err)
	requireEventually(t, am, acc.Id, activity.AccountMFAPeerLoginEnabled)
	on := mfaEvents(t, am, acc.Id, activity.AccountMFAPeerLoginEnabled)
	require.Len(t, on, 1)
	assert.Equal(t, "owner", on[0].InitiatorID)

	update = update.Copy()
	update.MFAEnforcePeerLogin = false
	_, err = am.UpdateAccountSettings(ctx, acc.Id, "owner", update)
	require.NoError(t, err)
	requireEventually(t, am, acc.Id, activity.AccountMFAPeerLoginDisabled)
	assert.Len(t, mfaEvents(t, am, acc.Id, activity.AccountMFAPeerLoginEnabled), 1, "an unchanged setting is not audited")
}

// A save that omits the device-login setting keeps the value stored when
// its transaction locks the settings row, not the one its caller read
// earlier: a change another replica committed in between survives.
func TestUpdateAccountSettings_PeerLoginMFAOmittedKeepsLockedValue(t *testing.T) {
	for _, stored := range []bool{false, true} {
		t.Run(fmt.Sprintf("stored %v", stored), func(t *testing.T) {
			am := newMFATestManager(t)
			ctx := context.Background()
			acc, err := am.GetOrCreateAccountByUser(ctx, "owner", "example.com")
			require.NoError(t, err)
			settings := acc.Settings.Copy()
			if settings.Extra == nil {
				settings.Extra = &types.ExtraSettings{}
			}
			settings.MFAEnforcePeerLogin = stored
			require.NoError(t, am.Store.SaveAccountSettings(ctx, store.LockingStrengthUpdate, acc.Id, settings))

			// The caller read the opposite value before another replica
			// changed it, and omitted the field.
			update := settings.Copy()
			update.MFAEnforcePeerLogin = !stored
			update.KeepMFAEnforcePeerLogin = true
			_, err = am.UpdateAccountSettings(ctx, acc.Id, "owner", update)
			require.NoError(t, err)

			got, err := am.Store.GetAccountSettings(ctx, store.LockingStrengthNone, acc.Id)
			require.NoError(t, err)
			assert.Equal(t, stored, got.MFAEnforcePeerLogin)
		})
	}
}

// Across replicas: A builds a save from settings it read with the gate on
// and omits the gate; B switches the gate off; A's save keeps B's value.
func TestUpdateAccountSettings_PeerLoginMFAOmittedTwoReplicas(t *testing.T) {
	replicas := newTwoReplicas(t)
	ctx := context.Background()
	acc, err := replicas.A.GetOrCreateAccountByUser(ctx, "owner", "example.com")
	require.NoError(t, err)
	settings := acc.Settings.Copy()
	if settings.Extra == nil {
		settings.Extra = &types.ExtraSettings{}
	}
	settings.MFAEnforcePeerLogin = true
	require.NoError(t, replicas.A.Store.SaveAccountSettings(ctx, store.LockingStrengthUpdate, acc.Id, settings))

	staleOnA := settings.Copy()
	staleOnA.KeepMFAEnforcePeerLogin = true

	off := settings.Copy()
	off.MFAEnforcePeerLogin = false
	_, err = replicas.B.UpdateAccountSettings(ctx, acc.Id, "owner", off)
	require.NoError(t, err)

	_, err = replicas.A.UpdateAccountSettings(ctx, acc.Id, "owner", staleOnA)
	require.NoError(t, err)
	got, err := replicas.B.Store.GetAccountSettings(ctx, store.LockingStrengthNone, acc.Id)
	require.NoError(t, err)
	assert.False(t, got.MFAEnforcePeerLogin, "A's stale value switched the gate back on")
}

// The gate's switch is audited once its transaction commits, even when the
// extra settings backend fails afterwards and the request reports an error.
func TestUpdateAccountSettings_PeerLoginMFAAuditSurvivesExtraFailure(t *testing.T) {
	am := newMFATestManager(t)
	ctx := context.Background()
	acc, err := am.GetOrCreateAccountByUser(ctx, "owner", "example.com")
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	failing := settings.NewMockManager(ctrl)
	failing.EXPECT().UpdateExtraSettings(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(false, errors.New("extra backend down")).AnyTimes()
	failing.EXPECT().GetExtraSettings(gomock.Any(), gomock.Any()).Return(&types.ExtraSettings{}, nil).AnyTimes()
	am.settingsManager = failing

	update := acc.Settings.Copy()
	update.MFAEnforcePeerLogin = true
	_, err = am.UpdateAccountSettings(ctx, acc.Id, "owner", update)
	require.Error(t, err)

	stored, err := am.Store.GetAccountSettings(ctx, store.LockingStrengthNone, acc.Id)
	require.NoError(t, err)
	require.True(t, stored.MFAEnforcePeerLogin, "the switch committed")
	requireEventually(t, am, acc.Id, activity.AccountMFAPeerLoginEnabled)
}
