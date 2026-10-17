package server

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLocalUsersEnabledFromEnv(t *testing.T) {
	for value, want := range map[string]bool{"": false, "false": false, "true": true, "1": true, "bogus": false} {
		t.Setenv(LocalUsersEnabledEnv, value)
		require.Equal(t, want, LocalUsersEnabledFromEnv(), "value %q", value)
	}
}
