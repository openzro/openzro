package cluster

import (
	"encoding/binary"
	"errors"
	"fmt"

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

// EncodeHello builds the HELLO payload — the dialer's own listen
// address as a UTF-8 string. The frame header carries the length;
// no extra prefix needed inside the payload.
func EncodeHello(listenAddr string) []byte {
	return []byte(listenAddr)
}

// DecodeHello parses a HELLO payload. Returns the announced listen
// address. Empty or oversized payloads are rejected — those would
// be from a misbehaving peer (or a future-protocol pod we shouldn't
// trust as a valid cluster member).
func DecodeHello(payload []byte) (string, error) {
	if len(payload) == 0 {
		return "", fmt.Errorf("%w: HELLO payload is empty", ErrMalformedPacket)
	}
	if len(payload) > MaxHelloAddressLen {
		return "", fmt.Errorf("%w: HELLO payload is %d bytes, max %d", ErrMalformedPacket, len(payload), MaxHelloAddressLen)
	}
	return string(payload), nil
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
