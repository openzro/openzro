package cluster

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/openzro/openzro/relay/messages"
)

// peerIDSize must match relay/messages.PeerID's array length. We
// don't pull that constant directly because messages doesn't export
// it — the size is fixed by the WG-derived hash format and unlikely
// to change without an ADR.
const peerIDSize = 36

// Payload sizes for fixed-shape messages. ReadFrame already
// validated the inbound length matches the one it read from the
// header, so the per-type decoders only check that the length
// matches the type's expected size.
const (
	whoHasPayloadSize  = peerIDSize                                                // 36
	iHavePayloadSize   = peerIDSize + 4                                            // 40
	openPayloadSize    = peerIDSize + peerIDSize + 4                               // 76
	openAckPayloadSize = 4 + 1                                                     // 5
	dataHeaderSize     = 4                                                         // channel_id, then opaque
	closePayloadSize   = 4                                                         // channel_id
)

// Errors returned when decoding a payload whose length doesn't
// match the type's expected shape, or whose embedded bytes are
// invalid. Wrapped — use errors.Is / errors.As to disambiguate.
var (
	ErrUnknownMsgType  = errors.New("cluster: unknown message type")
	ErrMalformedPacket = errors.New("cluster: malformed message payload")
)

// MaxHelloAddressLen caps the announced listen address. host:port
// in any sane K8s cluster is well under 100 bytes; 255 is generous
// and keeps the cap inside one byte for any future framing.
const MaxHelloAddressLen = 255

// HELLO v2 wire format:
//
//	┌──────────┬──────────┬──────────────┬───────────┬──────────┬─────────┐
//	│ uint8    │ uint8    │ bytes        │ uint64 BE │ uint8    │ bytes   │
//	│ version  │ addr_len │ announce_addr│ unix_sec  │ hmac_len │ hmac    │
//	└──────────┴──────────┴──────────────┴───────────┴──────────┴─────────┘
//
// version pins the format so future bumps don't need a flag day.
// timestamp + nonce-free design is enough — the receiver enforces a
// ±5 min replay window, and an attacker who replays an old HELLO
// just opens a duplicate stream that gets immediately deduped by
// the transport's announceAddr key.
//
// hmac_len is 0 (unsigned) or 32 (HMAC-SHA256). HMAC covers
// version || addr_len || announce_addr || timestamp. With an empty
// secret on either side, the unsigned form is used; asymmetric
// configs are rejected loudly.
const (
	helloVersionV2  = 2
	helloHMACSize   = sha256.Size
	helloMinPayload = 1 + 1 + 0 + 8 + 1 // version + addrLen + addr + ts + hmacLen

	// helloReplayWindow caps how stale a HELLO timestamp can be.
	// Clock skew between K8s pods is typically sub-second; 5 min
	// is generous and short enough that a captured HELLO has a
	// small window to be replayed.
	helloReplayWindow = 5 * time.Minute
)

// EncodeHello builds a v2 HELLO payload announcing this pod's
// listen address. When secret is non-empty, the payload includes
// an HMAC-SHA256 over the preceding fields so the receiver can
// authenticate the dialer without TLS. With empty secret the
// payload still uses v2 framing but carries hmac_len = 0 (unsigned
// — only acceptable on a NetworkPolicy-isolated backplane).
//
// ts is the wall-clock time stamped into the payload. Tests pass a
// fixed value; production callers pass time.Now().
func EncodeHello(listenAddr string, secret []byte, ts time.Time) ([]byte, error) {
	if len(listenAddr) > MaxHelloAddressLen {
		return nil, fmt.Errorf("%w: HELLO addr %d bytes, max %d", ErrMalformedPacket, len(listenAddr), MaxHelloAddressLen)
	}

	signedBody := make([]byte, 0, 2+len(listenAddr)+8)
	signedBody = append(signedBody, helloVersionV2)
	signedBody = append(signedBody, uint8(len(listenAddr)))
	signedBody = append(signedBody, listenAddr...)
	var tsBuf [8]byte
	binary.BigEndian.PutUint64(tsBuf[:], uint64(ts.Unix()))
	signedBody = append(signedBody, tsBuf[:]...)

	out := make([]byte, 0, len(signedBody)+1+helloHMACSize)
	out = append(out, signedBody...)
	if len(secret) > 0 {
		mac := hmac.New(sha256.New, secret)
		mac.Write(signedBody)
		out = append(out, helloHMACSize)
		out = mac.Sum(out)
	} else {
		out = append(out, 0)
	}
	return out, nil
}

// DecodeHello parses a v2 HELLO payload and returns the announced
// listen address. Authentication rules:
//
//   - secret non-empty: payload must carry a valid HMAC; missing
//     or mismatched HMAC is a hard reject.
//   - secret empty: payload must carry hmac_len=0; a signed HELLO
//     against an unconfigured receiver is a hard reject (asymmetric
//     config — the operator forgot the secret on this pod).
//
// Stale timestamps (outside ±helloReplayWindow) are rejected.
func DecodeHello(payload []byte, secret []byte, now time.Time) (string, error) {
	if len(payload) < helloMinPayload {
		return "", fmt.Errorf("%w: HELLO too short (%d bytes)", ErrMalformedPacket, len(payload))
	}
	if payload[0] != helloVersionV2 {
		return "", fmt.Errorf("%w: HELLO version %d, want %d", ErrMalformedPacket, payload[0], helloVersionV2)
	}
	addrLen := int(payload[1])
	if addrLen > MaxHelloAddressLen {
		return "", fmt.Errorf("%w: HELLO addr_len %d exceeds %d", ErrMalformedPacket, addrLen, MaxHelloAddressLen)
	}
	bodyEnd := 2 + addrLen + 8
	if len(payload) < bodyEnd+1 {
		return "", fmt.Errorf("%w: HELLO body truncated", ErrMalformedPacket)
	}
	addr := string(payload[2 : 2+addrLen])
	tsRaw := binary.BigEndian.Uint64(payload[2+addrLen : bodyEnd])
	ts := time.Unix(int64(tsRaw), 0)
	if d := now.Sub(ts); d > helloReplayWindow || d < -helloReplayWindow {
		return "", fmt.Errorf("%w: HELLO timestamp out of window: %s (skew=%s)", ErrMalformedPacket, ts, d)
	}

	hmacLen := int(payload[bodyEnd])
	sigStart := bodyEnd + 1
	if len(payload) < sigStart+hmacLen {
		return "", fmt.Errorf("%w: HELLO hmac field truncated", ErrMalformedPacket)
	}
	sig := payload[sigStart : sigStart+hmacLen]

	if len(secret) > 0 {
		if hmacLen == 0 {
			return "", fmt.Errorf("%w: peer sent unsigned HELLO but local pod requires auth", ErrMalformedPacket)
		}
		if hmacLen != helloHMACSize {
			return "", fmt.Errorf("%w: HELLO hmac %d bytes, want %d", ErrMalformedPacket, hmacLen, helloHMACSize)
		}
		mac := hmac.New(sha256.New, secret)
		mac.Write(payload[:bodyEnd])
		expect := mac.Sum(nil)
		if !hmac.Equal(expect, sig) {
			return "", fmt.Errorf("%w: HELLO hmac mismatch", ErrMalformedPacket)
		}
	} else if hmacLen != 0 {
		return "", fmt.Errorf("%w: peer sent signed HELLO but local pod has no auth secret (asymmetric config)", ErrMalformedPacket)
	}

	return addr, nil
}

// EncodeWhoHas builds the WHO_HAS payload for the given peer.
func EncodeWhoHas(target messages.PeerID) []byte {
	out := make([]byte, peerIDSize)
	copy(out, target[:])
	return out
}

// DecodeWhoHas parses a WHO_HAS payload.
func DecodeWhoHas(payload []byte) (messages.PeerID, error) {
	var pid messages.PeerID
	if len(payload) != whoHasPayloadSize {
		return pid, fmt.Errorf("%w: WHO_HAS payload is %d bytes, want %d", ErrMalformedPacket, len(payload), whoHasPayloadSize)
	}
	copy(pid[:], payload)
	return pid, nil
}

// EncodeIHave builds the I_HAVE payload. seqno lets the asker
// pick the most recent owner if two pods race to claim the peer
// (the loser will see its peer disconnect on the next conntrack
// churn anyway, but the broadcast resolves it earlier).
func EncodeIHave(target messages.PeerID, seqno uint32) []byte {
	out := make([]byte, iHavePayloadSize)
	copy(out, target[:])
	binary.BigEndian.PutUint32(out[peerIDSize:], seqno)
	return out
}

// DecodeIHave parses an I_HAVE payload.
func DecodeIHave(payload []byte) (messages.PeerID, uint32, error) {
	var pid messages.PeerID
	if len(payload) != iHavePayloadSize {
		return pid, 0, fmt.Errorf("%w: I_HAVE payload is %d bytes, want %d", ErrMalformedPacket, len(payload), iHavePayloadSize)
	}
	copy(pid[:], payload[:peerIDSize])
	seqno := binary.BigEndian.Uint32(payload[peerIDSize:])
	return pid, seqno, nil
}

// EncodeOpen requests a channel between two peers across pods.
// channelID is allocated by the asking pod from its own namespace
// — the receiving pod refers to the channel by the same id.
func EncodeOpen(src, dst messages.PeerID, channelID uint32) []byte {
	out := make([]byte, openPayloadSize)
	copy(out, src[:])
	copy(out[peerIDSize:], dst[:])
	binary.BigEndian.PutUint32(out[2*peerIDSize:], channelID)
	return out
}

// DecodeOpen parses an OPEN payload.
func DecodeOpen(payload []byte) (src, dst messages.PeerID, channelID uint32, _ error) {
	if len(payload) != openPayloadSize {
		return src, dst, 0, fmt.Errorf("%w: OPEN payload is %d bytes, want %d", ErrMalformedPacket, len(payload), openPayloadSize)
	}
	copy(src[:], payload[:peerIDSize])
	copy(dst[:], payload[peerIDSize:2*peerIDSize])
	channelID = binary.BigEndian.Uint32(payload[2*peerIDSize:])
	return src, dst, channelID, nil
}

// OpenStatus describes the outcome of a channel open attempt.
type OpenStatus uint8

const (
	OpenStatusReject OpenStatus = 0
	OpenStatusAccept OpenStatus = 1
)

// String makes OpenStatus log-friendly.
func (s OpenStatus) String() string {
	switch s {
	case OpenStatusAccept:
		return "ACCEPT"
	case OpenStatusReject:
		return "REJECT"
	default:
		return fmt.Sprintf("OpenStatus(%d)", uint8(s))
	}
}

// EncodeOpenAck builds the OPEN_ACK payload.
func EncodeOpenAck(channelID uint32, status OpenStatus) []byte {
	out := make([]byte, openAckPayloadSize)
	binary.BigEndian.PutUint32(out[:4], channelID)
	out[4] = uint8(status)
	return out
}

// DecodeOpenAck parses an OPEN_ACK payload.
func DecodeOpenAck(payload []byte) (uint32, OpenStatus, error) {
	if len(payload) != openAckPayloadSize {
		return 0, 0, fmt.Errorf("%w: OPEN_ACK payload is %d bytes, want %d", ErrMalformedPacket, len(payload), openAckPayloadSize)
	}
	channelID := binary.BigEndian.Uint32(payload[:4])
	status := OpenStatus(payload[4])
	return channelID, status, nil
}

// EncodeData builds the hot-path DATA payload. The caller owns the
// `bytes` slice; the encoder takes a copy because the frame writer
// may emit the result asynchronously.
func EncodeData(channelID uint32, bytes []byte) []byte {
	out := make([]byte, dataHeaderSize+len(bytes))
	binary.BigEndian.PutUint32(out[:dataHeaderSize], channelID)
	copy(out[dataHeaderSize:], bytes)
	return out
}

// DecodeData splits a DATA payload into the channel id and the
// inner relayed bytes. The returned slice aliases payload — the
// caller must copy if it wants to retain.
func DecodeData(payload []byte) (uint32, []byte, error) {
	if len(payload) < dataHeaderSize {
		return 0, nil, fmt.Errorf("%w: DATA payload is %d bytes, want ≥ %d", ErrMalformedPacket, len(payload), dataHeaderSize)
	}
	channelID := binary.BigEndian.Uint32(payload[:dataHeaderSize])
	return channelID, payload[dataHeaderSize:], nil
}

// EncodeClose builds a CLOSE payload.
func EncodeClose(channelID uint32) []byte {
	out := make([]byte, closePayloadSize)
	binary.BigEndian.PutUint32(out, channelID)
	return out
}

// DecodeClose parses a CLOSE payload.
func DecodeClose(payload []byte) (uint32, error) {
	if len(payload) != closePayloadSize {
		return 0, fmt.Errorf("%w: CLOSE payload is %d bytes, want %d", ErrMalformedPacket, len(payload), closePayloadSize)
	}
	return binary.BigEndian.Uint32(payload), nil
}
