package server

import (
	"testing"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func TestClientUpdateID(t *testing.T) {
	k1, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	k2, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}

	id1 := clientUpdateID(k1.String())
	if len(id1) != 64 { // sha256 hex
		t.Fatalf("expected 64-hex id, got %d chars", len(id1))
	}
	if id1 == k1.String() || id1 == k1.PublicKey().String() {
		t.Fatal("id must be a hash, not the raw/public key itself")
	}
	if clientUpdateID(k1.String()) != id1 {
		t.Fatal("id must be stable for the same key")
	}
	if clientUpdateID(k2.String()) == id1 {
		t.Fatal("different keys must yield different ids")
	}

	// Garbage that does not parse as a wg key still yields a stable,
	// opaque id (fallback path), never panics or empties.
	g := clientUpdateID("not-a-wireguard-key")
	if len(g) != 64 || g != clientUpdateID("not-a-wireguard-key") {
		t.Fatal("fallback id must be stable 64-hex")
	}
}
