package cluster

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Round-trip the encoder and decoder under every auth combination
// the wire format admits. Anything outside this matrix is a bug.
func TestEncodeDecodeHello_AuthMatrix(t *testing.T) {
	now := time.Now()
	addr := "10.0.5.7:7090"

	t.Run("both signed (matching secret)", func(t *testing.T) {
		secret := []byte("super-secret-cluster-key")
		buf, err := EncodeHello(addr, secret, now)
		require.NoError(t, err)

		got, err := DecodeHello(buf, secret, now)
		require.NoError(t, err)
		require.Equal(t, addr, got)
	})

	t.Run("both unsigned (empty secret)", func(t *testing.T) {
		buf, err := EncodeHello(addr, nil, now)
		require.NoError(t, err)

		got, err := DecodeHello(buf, nil, now)
		require.NoError(t, err)
		require.Equal(t, addr, got)
	})

	t.Run("dialer signed, receiver empty (asymmetric)", func(t *testing.T) {
		buf, err := EncodeHello(addr, []byte("dialer-key"), now)
		require.NoError(t, err)

		_, err = DecodeHello(buf, nil, now)
		require.ErrorIs(t, err, ErrMalformedPacket,
			"asymmetric config — receiver without secret must reject signed HELLO loudly")
	})

	t.Run("dialer empty, receiver requires secret (asymmetric)", func(t *testing.T) {
		buf, err := EncodeHello(addr, nil, now)
		require.NoError(t, err)

		_, err = DecodeHello(buf, []byte("receiver-key"), now)
		require.ErrorIs(t, err, ErrMalformedPacket,
			"unsigned HELLO must be rejected by a receiver with a secret")
	})

	t.Run("mismatched secrets", func(t *testing.T) {
		buf, err := EncodeHello(addr, []byte("alpha-key"), now)
		require.NoError(t, err)

		_, err = DecodeHello(buf, []byte("bravo-key"), now)
		require.ErrorIs(t, err, ErrMalformedPacket,
			"two pods with different secrets are an operator misconfiguration; refuse to form a stream")
	})
}

func TestDecodeHello_RejectsStaleTimestamp(t *testing.T) {
	addr := "10.0.5.7:7090"
	secret := []byte("k")

	// Encode at t0, decode at t0 + window + 1s.
	t0 := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	buf, err := EncodeHello(addr, secret, t0)
	require.NoError(t, err)

	stale := t0.Add(helloReplayWindow + time.Second)
	_, err = DecodeHello(buf, secret, stale)
	require.ErrorIs(t, err, ErrMalformedPacket,
		"HELLO timestamps older than the replay window must be rejected")

	// Future skew is symmetric — a HELLO from a pod with a clock
	// way ahead of ours is also a red flag.
	skewed := t0.Add(-(helloReplayWindow + time.Second))
	_, err = DecodeHello(buf, secret, skewed)
	require.ErrorIs(t, err, ErrMalformedPacket,
		"HELLO timestamps from an unreasonably-future clock must be rejected too")
}

func TestDecodeHello_RejectsTruncated(t *testing.T) {
	cases := [][]byte{
		nil,
		{},
		{helloVersionV2},                     // only version
		{helloVersionV2, 5, 'a', 'b'},        // addr_len says 5 but only 2 bytes
		{0x99, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, // wrong version
		make([]byte, helloMinPayload-1),      // 1 byte short of minimum
	}
	for _, c := range cases {
		_, err := DecodeHello(c, nil, time.Now())
		require.Error(t, err, "DecodeHello must reject %v", c)
		require.True(t, errors.Is(err, ErrMalformedPacket), "expected ErrMalformedPacket, got %v", err)
	}
}

// TestTransport_HelloHMACGate exercises the transport-level path:
// two transports with matching secrets form a stream; mismatched
// secrets are rejected at HELLO and never reach the streams map.
func TestTransport_HelloHMACGate(t *testing.T) {
	t.Run("matching secrets form a stream", func(t *testing.T) {
		secret := []byte("the-shared-key")

		tr1 := NewTransport("127.0.0.1:0", "", newCaptureHandler())
		tr1.SetAuthSecret(secret)
		require.NoError(t, tr1.ListenAndServe(context.Background()))
		t.Cleanup(tr1.Stop)

		tr2 := NewTransport("127.0.0.1:0", "", newCaptureHandler())
		tr2.SetAuthSecret(secret)
		require.NoError(t, tr2.ListenAndServe(context.Background()))
		t.Cleanup(tr2.Stop)

		_, err := tr2.Dial(context.Background(), tr1.listener.Addr().String())
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			return tr2.Stream(tr1.listener.Addr().String()) != nil
		}, time.Second, 5*time.Millisecond,
			"matching secrets must let HELLO succeed and the stream register")
	})

	t.Run("mismatched secrets reject the stream", func(t *testing.T) {
		tr1 := NewTransport("127.0.0.1:0", "", newCaptureHandler())
		tr1.SetAuthSecret([]byte("server-key"))
		require.NoError(t, tr1.ListenAndServe(context.Background()))
		t.Cleanup(tr1.Stop)

		tr2 := NewTransport("127.0.0.1:0", "", newCaptureHandler())
		tr2.SetAuthSecret([]byte("WRONG-key"))
		require.NoError(t, tr2.ListenAndServe(context.Background()))
		t.Cleanup(tr2.Stop)

		// Dial succeeds at TCP level (the bad HELLO is sent, then
		// the server drops the conn). The dialer's stream may
		// still be in its map briefly, but the read loop will
		// exit on the close; the SERVER side never registers.
		_, _ = tr2.Dial(context.Background(), tr1.listener.Addr().String())

		// Give the server time to reject + drop. After that the
		// server's streams map must be empty.
		time.Sleep(100 * time.Millisecond)
		require.Empty(t, tr1.Streams(),
			"server with a different secret must not register the bad-HMAC stream")
	})

	t.Run("unsigned dialer hits a signed server (asymmetric)", func(t *testing.T) {
		// Server requires auth; client doesn't sign. Server drops.
		tr1 := NewTransport("127.0.0.1:0", "", newCaptureHandler())
		tr1.SetAuthSecret([]byte("server-key"))
		require.NoError(t, tr1.ListenAndServe(context.Background()))
		t.Cleanup(tr1.Stop)

		tr2 := NewTransport("127.0.0.1:0", "", newCaptureHandler())
		// no SetAuthSecret on tr2 — unsigned dialer.
		require.NoError(t, tr2.ListenAndServe(context.Background()))
		t.Cleanup(tr2.Stop)

		_, _ = tr2.Dial(context.Background(), tr1.listener.Addr().String())

		time.Sleep(100 * time.Millisecond)
		require.Empty(t, tr1.Streams(),
			"signed server must reject unsigned dialer's HELLO")
	})
}
