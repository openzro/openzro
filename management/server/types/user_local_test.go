package types

import "testing"

// The REST update path edits a user through Copy(); dropping the local
// credential fields there would make every dashboard edit of a local user
// look like a state change and be refused by the store.
func TestUser_CopyKeepsLocalCredentialState(t *testing.T) {
	u := &User{Id: "u", LocalCredentialState: LocalCredentialDeactivated, LocalCleanupPending: true}
	c := u.Copy()
	if c.LocalCredentialState != LocalCredentialDeactivated || !c.LocalCleanupPending {
		t.Fatalf("Copy lost local credential fields: %+v", c)
	}
}

func TestLocalCredentialState_MustStayBlocked(t *testing.T) {
	tests := map[LocalCredentialState]bool{
		LocalCredentialNone:        false,
		LocalCredentialActive:      false,
		LocalCredentialPending:     true,
		LocalCredentialDeactivated: true,
		LocalCredentialTombstoned:  true,
	}
	for state, want := range tests {
		if got := state.MustStayBlocked(); got != want {
			t.Errorf("%q.MustStayBlocked() = %v, want %v", state, got, want)
		}
	}
}
