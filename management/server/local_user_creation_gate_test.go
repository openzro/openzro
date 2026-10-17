package server

import (
	"context"
	"testing"

	"github.com/openzro/openzro/management/server/account"
	"github.com/openzro/openzro/management/server/status"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #217 review: the opt-in gates creation only. Existing local users can
// still authenticate when it is off, so they must stay deprovisionable.
func TestLocalUserLifecycle_WorksWithCreationDisabled(t *testing.T) {
	m, accountID, creds := setupLocalUsers(t)
	ctx := context.Background()
	a := createLocal(t, m, accountID, "ana@example.com")
	b := createLocal(t, m, accountID, "bea@example.com")
	c := createLocal(t, m, accountID, "cid@example.com")

	m.localUserCreationEnabled = false

	_, err := m.SCIMCreateLocalUser(ctx, accountID, userID, localInput("dan@example.com"), goodPassword)
	requireStatusType(t, err, status.BadRequest)

	_, err = m.SCIMResetLocalPassword(ctx, accountID, userID, a.Id, newPassword, nil)
	require.NoError(t, err)
	inactive := false
	_, err = m.SCIMPatchUser(ctx, accountID, userID, b.Id, account.SCIMUserPatch{Active: &inactive})
	require.NoError(t, err)
	require.NoError(t, m.SCIMDeactivateUser(ctx, accountID, userID, c.Id))
	assert.Nil(t, creds.entry("bea@example.com"))
	assert.Nil(t, creds.entry("cid@example.com"))
}
