package admission

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.TempDir()+"/test.db"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	require.NoError(t, err)
	store, err := NewStore(db)
	require.NoError(t, err)
	return store
}

// TestGrant_RejectsMissingFields locks the audit-trail contract:
// every grant carries initiator + reason + expiry. Missing any of
// these produces a typed error so the API returns 400 instead of
// silently writing a row that breaks the auditor's report.
func TestGrant_RejectsMissingFields(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	future := time.Now().UTC().Add(time.Hour)

	cases := []struct {
		name string
		in   GrantInput
	}{
		{"no account", GrantInput{PeerID: "p", InitiatorID: "u", Reason: "r", ExpiresAt: future}},
		{"no peer", GrantInput{AccountID: "a", InitiatorID: "u", Reason: "r", ExpiresAt: future}},
		{"no initiator", GrantInput{AccountID: "a", PeerID: "p", Reason: "r", ExpiresAt: future}},
		{"no reason", GrantInput{AccountID: "a", PeerID: "p", InitiatorID: "u", ExpiresAt: future}},
		{"no expiry", GrantInput{AccountID: "a", PeerID: "p", InitiatorID: "u", Reason: "r"}},
		{"past expiry", GrantInput{AccountID: "a", PeerID: "p", InitiatorID: "u", Reason: "r", ExpiresAt: time.Now().UTC().Add(-time.Hour)}},
		{"too far", GrantInput{AccountID: "a", PeerID: "p", InitiatorID: "u", Reason: "r", ExpiresAt: time.Now().UTC().Add(MaxBypassDuration + time.Hour)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.Grant(ctx, tc.in)
			assert.Error(t, err, "input %s should be rejected", tc.name)
		})
	}
}

// TestGrant_HappyPath proves the full round-trip: create, list,
// IsActive returns true with the row.
func TestGrant_HappyPath(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	expires := time.Now().UTC().Add(24 * time.Hour)

	row, err := s.Grant(ctx, GrantInput{
		AccountID:   "acct-1",
		PeerID:      "peer-1",
		InitiatorID: "admin-1",
		Reason:      "CEO laptop pending Intune re-enroll",
		ExpiresAt:   expires,
	})
	require.NoError(t, err)
	assert.NotZero(t, row.ID)

	active, fetched, err := s.IsActive(ctx, "acct-1", "peer-1")
	require.NoError(t, err)
	assert.True(t, active)
	require.NotNil(t, fetched)
	assert.Equal(t, "admin-1", fetched.InitiatorID)
	assert.Equal(t, "CEO laptop pending Intune re-enroll", fetched.Reason)

	rows, err := s.List(ctx, "acct-1")
	require.NoError(t, err)
	assert.Len(t, rows, 1)
}

// TestGrant_TenantIsolation defends against cross-tenant ID
// guessing — account A's grant must not affect account B's
// IsActive lookup on the same peer ID.
func TestGrant_TenantIsolation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	expires := time.Now().UTC().Add(time.Hour)

	_, err := s.Grant(ctx, GrantInput{
		AccountID: "acct-a", PeerID: "peer-x", InitiatorID: "u", Reason: "r", ExpiresAt: expires,
	})
	require.NoError(t, err)

	active, _, err := s.IsActive(ctx, "acct-b", "peer-x")
	require.NoError(t, err)
	assert.False(t, active, "tenant B must not see tenant A's bypass")

	// Revoke from B for the same peer ID returns NotFound.
	_, err = s.Revoke(ctx, "acct-b", "peer-x")
	assert.ErrorIs(t, err, ErrNotFound)
}

// TestGrant_ReplacesExisting locks the "replace on regrant"
// behavior: a second grant for the same (account, peer) supersedes
// the first. Without this, the IsActive lookup would have to scan
// rows and pick the most recent, which is more expensive.
func TestGrant_ReplacesExisting(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	expires := time.Now().UTC().Add(time.Hour)

	first, err := s.Grant(ctx, GrantInput{
		AccountID: "acct", PeerID: "peer", InitiatorID: "u1", Reason: "first", ExpiresAt: expires,
	})
	require.NoError(t, err)

	second, err := s.Grant(ctx, GrantInput{
		AccountID: "acct", PeerID: "peer", InitiatorID: "u2", Reason: "second", ExpiresAt: expires,
	})
	require.NoError(t, err)
	assert.NotEqual(t, first.ID, second.ID)

	rows, err := s.List(ctx, "acct")
	require.NoError(t, err)
	require.Len(t, rows, 1, "second grant should replace the first")
	assert.Equal(t, "second", rows[0].Reason)
}

// TestSweepExpired_RemovesPastRows proves the worker contract:
// rows with ExpiresAt in the past are returned (so the caller can
// emit audit events) AND deleted.
func TestSweepExpired_RemovesPastRows(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Bypass the validation in Grant by inserting directly so we can
	// populate a "past expiry" row. The validation refuses past
	// dates by design; the store has to handle them anyway because
	// time passes between Grant and SweepExpired.
	past := time.Now().UTC().Add(-time.Hour)
	require.NoError(t, s.db.Create(&PeerAdmissionBypass{
		AccountID: "a", PeerID: "p1", InitiatorID: "u", Reason: "old",
		GrantedAt: past.Add(-time.Hour), ExpiresAt: past,
	}).Error)
	// Plus one still-active row that must NOT be swept.
	require.NoError(t, s.db.Create(&PeerAdmissionBypass{
		AccountID: "a", PeerID: "p2", InitiatorID: "u", Reason: "live",
		GrantedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour),
	}).Error)

	expired, err := s.SweepExpired(ctx, time.Now().UTC())
	require.NoError(t, err)
	require.Len(t, expired, 1)
	assert.Equal(t, "p1", expired[0].PeerID)

	// Active row remains.
	rows, err := s.List(ctx, "a")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "p2", rows[0].PeerID)
}

// TestHasGroupOverlap covers the group-scope short-circuit.
func TestHasGroupOverlap(t *testing.T) {
	cases := []struct {
		name        string
		peerGroups  []string
		exemptGroups []string
		want        bool
	}{
		{"empty peer", nil, []string{"infra"}, false},
		{"empty exempt", []string{"users"}, nil, false},
		{"no overlap", []string{"users"}, []string{"infra"}, false},
		{"single overlap", []string{"users", "infra"}, []string{"infra"}, true},
		{"multi overlap", []string{"a", "b"}, []string{"b", "c"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, HasGroupOverlap(tc.peerGroups, tc.exemptGroups))
		})
	}
}

// TestIsActive_ExpiredRowReportsFalse — a row that has expired but
// the worker has not yet swept it is reported inactive. Eventual
// consistency is fine for the audit log; the gate behavior must be
// instant.
func TestIsActive_ExpiredRowReportsFalse(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, s.db.Create(&PeerAdmissionBypass{
		AccountID: "a", PeerID: "p", InitiatorID: "u", Reason: "stale",
		GrantedAt: time.Now().UTC().Add(-2 * time.Hour),
		ExpiresAt: time.Now().UTC().Add(-time.Hour),
	}).Error)

	active, _, err := s.IsActive(ctx, "a", "p")
	require.NoError(t, err)
	assert.False(t, active)
}
