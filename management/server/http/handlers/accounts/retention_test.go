package accounts

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/management/server/types"
)

// The traffic page needs the boundary before it asks a question, not
// after, so it rides along on the payload that page already waits for.
// If this stops being reported the page goes quiet about exactly the
// queries worth warning about -- silently, because a missing field and
// an all-hot window look the same to the client.
func TestAccountReportsHotRetention(t *testing.T) {
	t.Setenv("OPENZRO_FLOW_RETENTION", "720h")

	got := toAccountResponse("acct-1", &types.Settings{}, &types.AccountMeta{}, &types.AccountOnboarding{}, true)
	require.NotNil(t, got.Settings.Extra)
	require.NotNil(t, got.Settings.Extra.NetworkTrafficHotRetentionSeconds)
	require.Equal(t, 720*3600, *got.Settings.Extra.NetworkTrafficHotRetentionSeconds)
}

// Reported exactly, and this is the whole reason the field is seconds.
//
// The client uses this as the boundary itself. An earlier version
// reported hours rounded up, which pushed the boundary further into the
// past: a 90-minute retention was published as 2h, and a query starting
// 100 minutes ago reached the archive on the server while the page,
// comparing against 2h, said nothing. Silent in exactly the window worth
// warning about.
func TestAccountReportsRetentionExactly(t *testing.T) {
	t.Setenv("OPENZRO_FLOW_RETENTION", "90m")

	got := toAccountResponse("acct-1", &types.Settings{}, &types.AccountMeta{}, &types.AccountOnboarding{}, true)
	require.Equal(t, 5400, *got.Settings.Extra.NetworkTrafficHotRetentionSeconds,
		"the boundary must not be rounded; rounding it moves it")
}

// A deployment with no archive answers a window past the boundary with
// nothing, not slowly. Promising a wait there describes a tier the
// operator does not run.
func TestAccountReportsWhetherArchiveReadsAreEnabled(t *testing.T) {
	on := toAccountResponse("a", &types.Settings{}, &types.AccountMeta{}, &types.AccountOnboarding{}, true)
	require.True(t, *on.Settings.Extra.NetworkTrafficArchiveReadsEnabled)

	off := toAccountResponse("a", &types.Settings{}, &types.AccountMeta{}, &types.AccountOnboarding{}, false)
	require.False(t, *off.Settings.Extra.NetworkTrafficArchiveReadsEnabled)
}

// An unset variable has to report the server's own default rather than
// nothing, or the client cannot tell "no archive" from "no answer".
func TestAccountReportsDefaultRetention(t *testing.T) {
	t.Setenv("OPENZRO_FLOW_RETENTION", "")

	got := toAccountResponse("acct-1", &types.Settings{}, &types.AccountMeta{}, &types.AccountOnboarding{}, true)
	require.Equal(t, int((7*24*time.Hour)/time.Second), *got.Settings.Extra.NetworkTrafficHotRetentionSeconds)
}
