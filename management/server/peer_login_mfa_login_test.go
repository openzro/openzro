package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/telemetry"
	"github.com/openzro/openzro/management/server/types"
)

// gateOn switches the device-login gate on for local users, bypassing the
// activation check, and turns on login expiration.
func (f *peerLoginFixture) gateOn(t *testing.T, mutate func(*types.Settings)) {
	t.Helper()
	ctx := context.Background()
	settings, err := f.am.Store.GetAccountSettings(ctx, store.LockingStrengthNone, f.accountID)
	require.NoError(t, err)
	settings.MFAEnforcePeerLogin = true
	settings.MFAEnforceLocal = true
	settings.PeerLoginExpirationEnabled = true
	settings.PeerLoginExpiration = time.Hour
	if settings.Extra == nil {
		settings.Extra = &types.ExtraSettings{}
	}
	if mutate != nil {
		mutate(settings)
	}
	require.NoError(t, f.am.Store.SaveAccountSettings(ctx, store.LockingStrengthUpdate, f.accountID, settings))
}

func (f *peerLoginFixture) login(key, connector string, labels ...string) error {
	_, _, _, err := f.am.LoginPeer(context.Background(), types.PeerLogin{
		WireGuardPubKey: key,
		Meta:            nbpeer.PeerSystemMeta{Hostname: "ana-laptop", GoOS: "linux", OS: "Ubuntu"},
		UserID:          f.userID,
		ConnectorID:     connector,
		JWTIssuedAt:     f.clock.Now(),
		ConnectionIP:    net.IP{203, 0, 113, 7},
		ExtraDNSLabels:  labels,
	})
	return err
}

func (f *peerLoginFixture) peerExists(t *testing.T, key string) bool {
	t.Helper()
	_, err := f.am.Store.GetPeerByPeerPubKey(context.Background(), store.LockingStrengthNone, key)
	if err == nil {
		return true
	}
	s, ok := status.FromError(err)
	require.True(t, ok && s.Type() == status.NotFound, "unexpected error: %v", err)
	return false
}

// requireApproval logs in, expects the refusal that points to an approval,
// and returns it.
func (f *peerLoginFixture) requireApproval(t *testing.T, key string) *PeerLoginMFARequiredError {
	t.Helper()
	err := f.login(key, "local")
	var required *PeerLoginMFARequiredError
	require.True(t, errors.As(err, &required), "want an approval refusal, got %v", err)
	require.NotEmpty(t, required.Challenge.Token)
	return required
}

func (f *peerLoginFixture) approve(t *testing.T, token string) {
	t.Helper()
	outcome, err := f.am.ApprovePeerLoginMFA(context.Background(), token, f.code(t))
	require.NoError(t, err)
	require.True(t, outcome.Approved)
	f.clock.Advance(30 * time.Second)
}

// expire makes the peer's login older than the account's expiration.
func (f *peerLoginFixture) expire(t *testing.T, key string) {
	t.Helper()
	ctx := context.Background()
	peer, err := f.am.Store.GetPeerByPeerPubKey(ctx, store.LockingStrengthNone, key)
	require.NoError(t, err)
	past := time.Now().UTC().Add(-2 * time.Hour)
	peer.LastLogin = &past
	require.NoError(t, f.am.Store.SavePeer(ctx, store.LockingStrengthUpdate, f.accountID, peer))
}

// A new device registers only after the user approves the login with their
// second factor; the approval is used once, by that registration.
func TestLoginPeer_MFAGate_NewPeer(t *testing.T) {
	f := newPeerLoginFixture(t, "ana")
	f.gateOn(t, nil)

	first := f.requireApproval(t, wgKeyA)
	assert.False(t, f.peerExists(t, wgKeyA), "a refused login registers nothing")
	again := f.requireApproval(t, wgKeyA)
	assert.Equal(t, first.Challenge.ApprovalID, again.Challenge.ApprovalID, "a retry waits on the same approval")
	assert.True(t, f.record(t, first.Challenge.ApprovalID).NewPeer)

	f.approve(t, first.Challenge.Token)
	require.NoError(t, f.login(wgKeyA, "local"))
	assert.True(t, f.peerExists(t, wgKeyA))
	assert.NotNil(t, f.record(t, first.Challenge.ApprovalID).ConsumedAt, "the registration used the approval")

	require.NoError(t, f.login(wgKeyA, "local"), "a peer whose login is valid is not asked again")
}

// A login-expired device is asked again; the approval re-activates it.
func TestLoginPeer_MFAGate_ExpiredPeer(t *testing.T) {
	f := newPeerLoginFixture(t, "ana")
	f.gateOn(t, nil)
	f.approve(t, f.requireApproval(t, wgKeyA).Challenge.Token)
	require.NoError(t, f.login(wgKeyA, "local"))

	f.expire(t, wgKeyA)
	required := f.requireApproval(t, wgKeyA)
	assert.False(t, f.record(t, required.Challenge.ApprovalID).NewPeer)
	f.approve(t, required.Challenge.Token)
	require.NoError(t, f.login(wgKeyA, "local"))
	assert.NotNil(t, f.record(t, required.Challenge.ApprovalID).ConsumedAt)
}

// A login that fails after using the approval rolls back, and the approval
// stays usable for the next attempt.
func TestLoginPeer_MFAGate_RollbackKeepsApproval(t *testing.T) {
	f := newPeerLoginFixture(t, "ana")
	f.gateOn(t, nil)
	f.approve(t, f.requireApproval(t, wgKeyA).Challenge.Token)
	require.NoError(t, f.login(wgKeyA, "local"))
	f.expire(t, wgKeyA)
	required := f.requireApproval(t, wgKeyA)
	f.approve(t, required.Challenge.Token)

	// The peer was registered without a setup key that allows extra DNS
	// labels, so the login fails after the gate.
	err := f.login(wgKeyA, "local", "extra")
	require.Error(t, err)
	var mfaRequired *PeerLoginMFARequiredError
	require.False(t, errors.As(err, &mfaRequired), "the gate passed: %v", err)
	assert.Nil(t, f.record(t, required.Challenge.ApprovalID).ConsumedAt, "the failed login did not use the approval")

	require.NoError(t, f.login(wgKeyA, "local"))
	assert.NotNil(t, f.record(t, required.Challenge.ApprovalID).ConsumedAt)
}

// The gate applies only when the setting is on and the 2FA rule covers the
// login's connector.
func TestLoginPeer_MFAGate_NotRequired(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*types.Settings)
		connector string
	}{
		{"setting off", func(s *types.Settings) { s.MFAEnforcePeerLogin = false }, "local"},
		{"2FA rule off for the connector", nil, "google"},
		{"local rule off", func(s *types.Settings) { s.MFAEnforceLocal = false }, "local"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newPeerLoginFixture(t, "ana")
			f.gateOn(t, tc.mutate)
			require.NoError(t, f.login(wgKeyA, tc.connector))
			approvals, err := f.am.Store.GetUserPeerLoginMFAApprovals(context.Background(), store.LockingStrengthNone,
				f.accountID, f.userID, f.clock.Now())
			require.NoError(t, err)
			assert.Empty(t, approvals)
		})
	}
}

// A user with no 2FA is told to set it up; no approval is created.
func TestLoginPeer_MFAGate_EnrollmentRequired(t *testing.T) {
	f := newPeerLoginFixture(t, "ana")
	f.gateOn(t, nil)
	_, err := f.am.Store.DeleteUserMFA(context.Background(), store.LockingStrengthUpdate, f.userID)
	require.NoError(t, err)

	err = f.login(wgKeyA, "local")
	var enroll *PeerLoginMFAEnrollmentRequiredError
	require.True(t, errors.As(err, &enroll), "want an enrollment refusal, got %v", err)
	approvals, err := f.am.Store.GetUserPeerLoginMFAApprovals(context.Background(), store.LockingStrengthNone,
		f.accountID, f.userID, f.clock.Now())
	require.NoError(t, err)
	assert.Empty(t, approvals)
	assert.False(t, f.peerExists(t, wgKeyA))
}

// "Not me" keeps the device out until its record expires.
func TestLoginPeer_MFAGate_Denied(t *testing.T) {
	f := newPeerLoginFixture(t, "ana")
	f.gateOn(t, nil)
	required := f.requireApproval(t, wgKeyA)
	_, err := f.am.DenyPeerLoginMFA(context.Background(), required.Challenge.Token)
	require.NoError(t, err)

	err = f.login(wgKeyA, "local")
	var denied *PeerLoginMFADeniedError
	require.True(t, errors.As(err, &denied), "want a denial, got %v", err)
	assert.Positive(t, denied.RetryAfter)
}

// With the setting on and no MFA subsystem, the login fails closed.
func TestLoginPeer_MFAGate_FailsClosedWithoutSubsystem(t *testing.T) {
	f := newPeerLoginFixture(t, "ana")
	f.gateOn(t, nil)
	f.am.mfaSigner = nil

	err := f.login(wgKeyA, "local")
	s, ok := status.FromError(err)
	require.True(t, ok, "want a status error, got %v", err)
	assert.Equal(t, status.Internal, s.Type())
	assert.False(t, f.peerExists(t, wgKeyA))
}

// peerLoginMFAMeter swaps the manager's device-login 2FA metrics for ones
// read by the returned function: counter value by attribute value, and the
// number of time-to-approval samples.
func (f *peerLoginFixture) peerLoginMFAMeter(t *testing.T) func(name string) (map[string]int64, uint64) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	m, err := telemetry.NewPeerLoginMFAMetrics(context.Background(), provider.Meter("test"))
	require.NoError(t, err)
	f.am.peerLoginMFAMetrics = m
	return func(name string) (map[string]int64, uint64) {
		var rm metricdata.ResourceMetrics
		require.NoError(t, reader.Collect(context.Background(), &rm))
		counts := map[string]int64{}
		var samples uint64
		for _, sm := range rm.ScopeMetrics {
			for _, metric := range sm.Metrics {
				if metric.Name != name {
					continue
				}
				switch data := metric.Data.(type) {
				case metricdata.Sum[int64]:
					for _, dp := range data.DataPoints {
						for _, kv := range dp.Attributes.ToSlice() {
							counts[kv.Value.AsString()] += dp.Value
						}
					}
				case metricdata.Histogram[float64]:
					for _, dp := range data.DataPoints {
						samples += dp.Count
					}
				}
			}
		}
		return counts, samples
	}
}

// Each refusal is counted by reason (required ones are the client's polls),
// and each approval outcome once; the wait until approval is recorded.
func TestLoginPeer_MFAGate_Metrics(t *testing.T) {
	f := newPeerLoginFixture(t, "ana")
	f.gateOn(t, nil)
	read := f.peerLoginMFAMeter(t)

	required := f.requireApproval(t, wgKeyA)
	poll := f.requireApproval(t, wgKeyA)
	require.Equal(t, required.Challenge.ApprovalID, poll.Challenge.ApprovalID, "a poll waits on the same approval")
	outcome, err := f.am.ApprovePeerLoginMFA(context.Background(), required.Challenge.Token, "000000")
	require.NoError(t, err)
	require.False(t, outcome.Approved)
	f.approve(t, required.Challenge.Token)
	f.approve(t, required.Challenge.Token)
	require.NoError(t, f.login(wgKeyA, "local"))

	const wgKeyB = "Pxc1ylvTfBm7y8MbEZvyMMYw5LwMqLbqvPxbTN0cCVQ="
	other := f.requireApproval(t, wgKeyB)
	_, err = f.am.DenyPeerLoginMFA(context.Background(), other.Challenge.Token)
	require.NoError(t, err)
	_ = f.login(wgKeyB, "local")

	refusals, _ := read("management.peer.login.mfa.refusal.counter")
	assert.Equal(t, map[string]int64{"required": 3, "denied": 1}, refusals)
	outcomes, _ := read("management.peer.login.mfa.outcome.counter")
	assert.Equal(t, map[string]int64{"failed": 1, "approved": 1, "denied": 1}, outcomes,
		"a retried approval is counted once")
	_, samples := read("management.peer.login.mfa.time.to.approval.ms")
	assert.Equal(t, uint64(1), samples)
}

// beforeTxStore runs before once, just before the next transaction
// starts: what another replica commits between a caller's earlier reads
// and its transaction.
type beforeTxStore struct {
	store.Store
	before func()
}

func (s *beforeTxStore) ExecuteInTransaction(ctx context.Context, operation func(store.Store) error) error {
	if before := s.before; before != nil {
		s.before = nil
		before()
	}
	return s.Store.ExecuteInTransaction(ctx, operation)
}

// enableGateBeforeNextTx switches the gate on, as another replica would,
// right before the login's transaction starts.
func (f *peerLoginFixture) enableGateBeforeNextTx(t *testing.T) {
	t.Helper()
	inner := f.am.Store
	f.am.Store = &beforeTxStore{Store: inner, before: func() {
		settings, err := inner.GetAccountSettings(context.Background(), store.LockingStrengthNone, f.accountID)
		require.NoError(t, err)
		settings.MFAEnforcePeerLogin = true
		require.NoError(t, inner.SaveAccountSettings(context.Background(), store.LockingStrengthUpdate, f.accountID, settings))
	}}
	t.Cleanup(func() { f.am.Store = inner })
}

// A registration decides the gate inside its transaction: a gate switched
// on after the login read the settings, but before it registered the
// device, still applies.
func TestLoginPeer_MFAGate_NewPeerSeesGateEnabledMeanwhile(t *testing.T) {
	f := newPeerLoginFixture(t, "ana")
	f.gateOn(t, func(s *types.Settings) { s.MFAEnforcePeerLogin = false })
	f.enableGateBeforeNextTx(t)

	required := f.requireApproval(t, wgKeyA)
	assert.Nil(t, f.record(t, required.Challenge.ApprovalID).ApprovedAt)
	assert.False(t, f.peerExists(t, wgKeyA), "the device registered without the approval")
}

// Re-activating an expired device also decides the gate inside its
// transaction.
func TestLoginPeer_MFAGate_ExpiredPeerSeesGateEnabledMeanwhile(t *testing.T) {
	f := newPeerLoginFixture(t, "ana")
	f.gateOn(t, func(s *types.Settings) { s.MFAEnforcePeerLogin = false })
	require.NoError(t, f.login(wgKeyA, "local"))
	f.expire(t, wgKeyA)
	f.enableGateBeforeNextTx(t)

	required := f.requireApproval(t, wgKeyA)
	assert.False(t, f.record(t, required.Challenge.ApprovalID).NewPeer)
}

// settingsReadHookStore runs onRead once, right after the first read of
// the account settings inside a transaction: the read the gate decides on,
// whether or not it locks.
type settingsReadHookStore struct {
	store.Store
	onRead func()
}

func (s *settingsReadHookStore) ExecuteInTransaction(ctx context.Context, operation func(store.Store) error) error {
	return s.Store.ExecuteInTransaction(ctx, func(tx store.Store) error {
		return operation(&settingsReadHookTx{Store: tx, parent: s})
	})
}

type settingsReadHookTx struct {
	store.Store
	parent *settingsReadHookStore
}

func (tx *settingsReadHookTx) GetAccountSettings(ctx context.Context, lock store.LockingStrength,
	accountID string) (*types.Settings, error) {
	settings, err := tx.Store.GetAccountSettings(ctx, lock, accountID)
	if onRead := tx.parent.onRead; err == nil && onRead != nil {
		tx.parent.onRead = nil
		onRead()
	}
	return settings, err
}

// An admin switching the gate on through replica B while replica A signs a
// device in under the gate's old value: B's save returns only after A's
// login commits, so once the activation returns no login decided under
// the old value can still commit, and the next one is gated.
func TestLoginPeer_MFAGate_ActivationWaitsForInflightLogin(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, f *peerLoginFixture)
	}{
		{"registration", func(*testing.T, *peerLoginFixture) {}},
		{"expired peer", func(t *testing.T, f *peerLoginFixture) {
			require.NoError(t, f.login(wgKeyA, "local"))
			f.expire(t, wgKeyA)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			replicas := newTwoReplicas(t)
			key := make([]byte, 32)
			_, err := rand.Read(key)
			require.NoError(t, err)
			for _, am := range []*DefaultAccountManager{replicas.A, replicas.B} {
				require.NoError(t, am.SetMFA(base64.StdEncoding.EncodeToString(key)))
			}
			f := newPeerLoginFixtureOn(t, replicas.A, "ana")
			f.gateOn(t, func(s *types.Settings) { s.MFAEnforcePeerLogin = false })
			tc.setup(t, f)

			// A different admin saves, so B never waits on the row of the
			// user A is signing in.
			seedMFAUser(t, replicas.B, f.accountID, "admin", types.UserRoleAdmin)
			enable, err := replicas.B.Store.GetAccountSettings(context.Background(), store.LockingStrengthNone, f.accountID)
			require.NoError(t, err)
			enable.MFAEnforcePeerLogin = true

			inner := replicas.A.Store
			saved := make(chan error, 1)
			savedDuringLogin := false
			replicas.A.Store = &settingsReadHookStore{Store: inner, onRead: func() {
				go func() {
					_, err := replicas.B.UpdateAccountSettings(context.Background(), f.accountID, "admin", enable)
					saved <- err
				}()
				// Give B's save time to return if nothing holds it back,
				// while A's login transaction is still open.
				select {
				case err := <-saved:
					savedDuringLogin = true
					saved <- err
				case <-time.After(300 * time.Millisecond):
				}
			}}
			t.Cleanup(func() { replicas.A.Store = inner })

			require.NoError(t, f.login(wgKeyA, "local"), "A's login read the gate off")
			assert.False(t, savedDuringLogin, "B's activation returned while A's login under the old value was open")
			select {
			case err := <-saved:
				require.NoError(t, err)
			case <-time.After(barrierTimeout):
				t.Fatal("B's activation never returned")
			}

			const wgKeyB = "Pxc1ylvTfBm7y8MbEZvyMMYw5LwMqLbqvPxbTN0cCVQ="
			f.requireApproval(t, wgKeyB)
		})
	}
}
