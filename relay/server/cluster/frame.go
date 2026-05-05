package cluster

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Frame is the wire format for inter-pod messages, per ADR-0014.
// One header byte for the type, two bytes (big-endian) for the
// payload length, then the payload itself. Max payload is 64 KiB,
// which comfortably exceeds a typical relayed WG packet (~1500 B)
// while keeping the buffer footprint per stream bounded.
//
//	┌─────────┬───────────────┬──────────────────┐
//	│ uint8   │ uint16 BE     │ payload bytes    │
//	│ msgType │ payload_len   │ (≤ 65535 bytes)  │
//	└─────────┴───────────────┴──────────────────┘
const (
	frameHeaderSize = 3
	maxPayloadSize  = 1 << 16 // 64 KiB, exclusive max enforced as ≤ 65535
)

// MsgType identifies the inter-pod control / data message.
//
// Numbering is split into ranges so future additions can extend the
// protocol without colliding:
//
//	0x00–0x0F  control: discovery / lookup
//	0x10–0x1F  channel lifecycle and data
//	0x20–0x2F  health
type MsgType uint8

const (
	// MsgHello is the very first frame on a freshly-dialed inter-
	// pod connection. The dialer announces the address it listens
	// on (`host:port`), so the accepting pod can index the stream
	// by that logical address rather than by the connection's
	// ephemeral source port. Without it, accepted streams would be
	// keyed by an ephemeral the receiver can't dial back to —
	// future forward attempts would fail.
	// Payload: UTF-8 listen address, length prefixed by the frame
	// header itself (≤ 255 bytes is more than any host:port needs).
	MsgHello MsgType = 0x00

	// MsgWhoHas asks every other pod whether they have a given peer
	// connected. Sent on a local-store miss when forwarding to a
	// peer this pod doesn't own. Payload: a fixed-size PeerID.
	MsgWhoHas MsgType = 0x01

	// MsgIHave answers a WHO_HAS positively, carrying a sequence
	// number so a temporarily split-brained "two pods both think
	// they own peer X" race resolves to the most recent owner.
	// Payload: PeerID (36) + uint32 seqno (4) = 40 bytes.
	MsgIHave MsgType = 0x02

	// MsgOpen establishes a per-channel forward path between two
	// peers. The asking pod proposes a channel_id from its own
	// space; the answering pod accepts with MsgOpenAck or rejects.
	// Payload: src PeerID (36) + dst PeerID (36) + uint32 channel_id.
	MsgOpen MsgType = 0x10

	// MsgOpenAck confirms (status=1) or rejects (status=0) a
	// channel open. Reject reasons are intentionally not encoded —
	// the caller retries with a fresh broadcast on miss.
	// Payload: uint32 channel_id (4) + uint8 status (1) = 5 bytes.
	MsgOpenAck MsgType = 0x11

	// MsgData is the hot-path forward of relayed bytes between two
	// pods. Payload: uint32 channel_id (4) + opaque bytes.
	MsgData MsgType = 0x12

	// MsgClose tears down a channel from either side. Idempotent —
	// duplicate closes are silently dropped by the receiver.
	// Payload: uint32 channel_id (4).
	MsgClose MsgType = 0x13

	// MsgFwd carries an entire relay transport message that needs
	// to be delivered to a peer connected to a different pod. The
	// payload is the bytes the originating pod would have written
	// to the local peer — destination peer ID is embedded in the
	// transport msg header, so the receiving pod just unmarshals
	// the dst id, looks it up locally, and forwards. Stateless;
	// every packet is independently routed and there's no
	// per-(src, dst) channel to keep alive.
	MsgFwd MsgType = 0x14

	// MsgPing / MsgPong keep the long-lived TCP stream warm and
	// detect partitions earlier than the kernel TCP keepalive
	// would. Empty payload.
	MsgPing MsgType = 0x20
	MsgPong MsgType = 0x21
)

// Errors returned by frame encode / decode. Callers use errors.Is
// to discriminate; do not depend on message text.
var (
	ErrPayloadTooLarge = errors.New("cluster: frame payload exceeds 64 KiB")
	ErrShortHeader     = errors.New("cluster: incomplete frame header")
	ErrShortPayload    = errors.New("cluster: incomplete frame payload")
)

// String makes MsgType safer to log without leaking surprise values.
func (t MsgType) String() string {
	switch t {
	case MsgHello:
		return "HELLO"
	case MsgWhoHas:
		return "WHO_HAS"
	case MsgIHave:
		return "I_HAVE"
	case MsgOpen:
		return "OPEN"
	case MsgOpenAck:
		return "OPEN_ACK"
	case MsgData:
		return "DATA"
	case MsgClose:
		return "CLOSE"
	case MsgFwd:
		return "FWD"
	case MsgPing:
		return "PING"
	case MsgPong:
		return "PONG"
	default:
		return fmt.Sprintf("MsgType(0x%02x)", uint8(t))
	}
}

// WriteFrame writes a single framed message to w. The payload may
// be nil for types that carry no data (PING, PONG). Returns
// ErrPayloadTooLarge if payload exceeds 65535 bytes — split or use
// a different message type.
func WriteFrame(w io.Writer, t MsgType, payload []byte) error {
	if len(payload) >= maxPayloadSize {
		return fmt.Errorf("%w: %d bytes (max %d)", ErrPayloadTooLarge, len(payload), maxPayloadSize-1)
	}

	var hdr [frameHeaderSize]byte
	hdr[0] = uint8(t)
	binary.BigEndian.PutUint16(hdr[1:], uint16(len(payload)))

	// Single Write per frame would be ideal but io.Writer doesn't
	// guarantee atomicity; the caller passes a per-stream lock. We
	// emit header + payload back-to-back; the contract is that the
	// caller serialises writes per stream.
	if _, err := w.Write(hdr[:]); err != nil {
		return fmt.Errorf("write frame header: %w", err)
	}
	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return fmt.Errorf("write frame payload: %w", err)
		}
	}
	return nil
}

// ReadFrame reads a single framed message from r. Returns the
// message type, the payload (may be empty, never nil), and an
// error.
//
// On a partial read the returned error wraps io.ErrUnexpectedEOF
// (or io.EOF on a clean stream end), which lets the caller
// distinguish "remote closed cleanly" from "partial frame". The
// payload buffer is owned by the caller after return — read loops
// can pool it if they want.
func ReadFrame(r io.Reader) (MsgType, []byte, error) {
	var hdr [frameHeaderSize]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return 0, nil, fmt.Errorf("%w: %v", ErrShortHeader, err)
		}
		return 0, nil, err
	}

	t := MsgType(hdr[0])
	plen := binary.BigEndian.Uint16(hdr[1:])

	if plen == 0 {
		return t, nil, nil
	}

	payload := make([]byte, plen)
	if _, err := io.ReadFull(r, payload); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return 0, nil, fmt.Errorf("%w: type=%s wanted=%d: %v", ErrShortPayload, t, plen, err)
		}
		return 0, nil, err
	}
	return t, payload, nil
}
