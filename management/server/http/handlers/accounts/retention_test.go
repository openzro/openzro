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

	got := toAccountResponse("acct-1", &types.Settings{}, &types.AccountMeta{}, &types.AccountOnboarding{})
	require.NotNil(t, got.Settings.Extra)
	require.NotNil(t, got.Settings.Extra.NetworkTrafficHotRetentionHours)
	require.Equal(t, 720, *got.Settings.Extra.NetworkTrafficHotRetentionHours)
}

// Rounded up, deliberately. A window reaching even a minute past the
// boundary is answered from the archive, so rounding down would leave
// the client quiet about a query that is about to take seconds.
func TestAccountRoundsRetentionUp(t *testing.T) {
	t.Setenv("OPENZRO_FLOW_RETENTION", "90m")

	got := toAccountResponse("acct-1", &types.Settings{}, &types.AccountMeta{}, &types.AccountOnboarding{})
	require.Equal(t, 2, *got.Settings.Extra.NetworkTrafficHotRetentionHours,
		"90 minutes is two hours' worth of boundary, not one")
}

// An unset variable has to report the server's own default rather than
// nothing, or the client cannot tell "no archive" from "no answer".
func TestAccountReportsDefaultRetention(t *testing.T) {
	t.Setenv("OPENZRO_FLOW_RETENTION", "")

	got := toAccountResponse("acct-1", &types.Settings{}, &types.AccountMeta{}, &types.AccountOnboarding{})
	require.Equal(t, int((7*24*time.Hour)/time.Hour), *got.Settings.Extra.NetworkTrafficHotRetentionHours)
}
