package cluster

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/openzro/openzro/relay/messages"
)

func TestWriteReadFrame_RoundTrip(t *testing.T) {
	cases := []struct {
		name    string
		msgType MsgType
		payload []byte
	}{
		{"empty payload", MsgPing, nil},
		{"empty pong", MsgPong, []byte{}},
		{"single byte", MsgWhoHas, []byte{0x42}},
		{"large payload", MsgData, bytes.Repeat([]byte{'x'}, 60000)},
		{"max payload-1", MsgData, bytes.Repeat([]byte{'y'}, 65534)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, WriteFrame(&buf, tc.msgType, tc.payload))

			gotType, gotPayload, err := ReadFrame(&buf)
			require.NoError(t, err)
			require.Equal(t, tc.msgType, gotType)
			if len(tc.payload) == 0 {
				require.Empty(t, gotPayload)
			} else {
				require.Equal(t, tc.payload, gotPayload)
			}
		})
	}
}

func TestWriteFrame_PayloadTooLarge(t *testing.T) {
	var buf bytes.Buffer
	tooBig := bytes.Repeat([]byte{'x'}, maxPayloadSize)
	err := WriteFrame(&buf, MsgData, tooBig)
	require.ErrorIs(t, err, ErrPayloadTooLarge)
	require.Zero(t, buf.Len(), "no bytes should leak when the payload is rejected")
}

func TestReadFrame_PartialHeader(t *testing.T) {
	r := bytes.NewReader([]byte{0x01}) // only 1 of 3 header bytes
	_, _, err := ReadFrame(r)
	require.ErrorIs(t, err, ErrShortHeader)
}

func TestReadFrame_PartialPayload(t *testing.T) {
	// header claims 10-byte payload, only 4 bytes follow
	var buf bytes.Buffer
	buf.Write([]byte{byte(MsgData), 0x00, 0x0A})
	buf.Write([]byte{1, 2, 3, 4})
	_, _, err := ReadFrame(&buf)
	require.ErrorIs(t, err, ErrShortPayload)
}

func TestReadFrame_CleanEOFNotShortHeader(t *testing.T) {
	// Empty stream: ReadFrame must return io.EOF, not ErrShortHeader,
	// so the read loop can distinguish "remote closed cleanly" from
	// "frame truncated in flight".
	_, _, err := ReadFrame(strings.NewReader(""))
	require.ErrorIs(t, err, io.EOF)
}

func TestMsgType_StringIsLogSafe(t *testing.T) {
	require.Equal(t, "WHO_HAS", MsgWhoHas.String())
	require.Equal(t, "DATA", MsgData.String())
	require.Equal(t, "MsgType(0xff)", MsgType(0xff).String(),
		"unknown types must format as hex so logs don't drop surprise values")
}

func TestEncodeDecode_MessageRoundTrips(t *testing.T) {
	var pidA, pidB messages.PeerID
	for i := range pidA {
		pidA[i] = byte(i)
		pidB[i] = byte(255 - i)
	}

	t.Run("WHO_HAS", func(t *testing.T) {
		got, err := DecodeWhoHas(EncodeWhoHas(pidA))
		require.NoError(t, err)
		require.Equal(t, pidA, got)
	})

	t.Run("I_HAVE", func(t *testing.T) {
		gotPID, gotSeq, err := DecodeIHave(EncodeIHave(pidA, 0xDEADBEEF))
		require.NoError(t, err)
		require.Equal(t, pidA, gotPID)
		require.Equal(t, uint32(0xDEADBEEF), gotSeq)
	})

	t.Run("OPEN", func(t *testing.T) {
		s, d, ch, err := DecodeOpen(EncodeOpen(pidA, pidB, 7))
		require.NoError(t, err)
		require.Equal(t, pidA, s)
		require.Equal(t, pidB, d)
		require.Equal(t, uint32(7), ch)
	})

	t.Run("OPEN_ACK accept", func(t *testing.T) {
		ch, st, err := DecodeOpenAck(EncodeOpenAck(7, OpenStatusAccept))
		require.NoError(t, err)
		require.Equal(t, uint32(7), ch)
		require.Equal(t, OpenStatusAccept, st)
	})

	t.Run("DATA", func(t *testing.T) {
		payload := []byte("hello relayed bytes")
		ch, body, err := DecodeData(EncodeData(99, payload))
		require.NoError(t, err)
		require.Equal(t, uint32(99), ch)
		require.Equal(t, payload, body)
	})

	t.Run("CLOSE", func(t *testing.T) {
		ch, err := DecodeClose(EncodeClose(0xCAFE))
		require.NoError(t, err)
		require.Equal(t, uint32(0xCAFE), ch)
	})
}

func TestDecode_RejectsWrongLengths(t *testing.T) {
	cases := []struct {
		name string
		fn   func() error
	}{
		{"WHO_HAS short", func() error { _, e := DecodeWhoHas([]byte{1, 2, 3}); return e }},
		{"I_HAVE short", func() error { _, _, e := DecodeIHave([]byte{1, 2, 3}); return e }},
		{"OPEN short", func() error { _, _, _, e := DecodeOpen([]byte{1, 2, 3}); return e }},
		{"OPEN_ACK short", func() error { _, _, e := DecodeOpenAck([]byte{1, 2}); return e }},
		{"DATA short", func() error { _, _, e := DecodeData([]byte{1, 2}); return e }},
		{"CLOSE wrong size", func() error { _, e := DecodeClose([]byte{1, 2, 3, 4, 5}); return e }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fn()
			require.Error(t, err)
			require.True(t, errors.Is(err, ErrMalformedPacket),
				"decode of malformed payload must wrap ErrMalformedPacket so callers can branch on it; got %v", err)
		})
	}
}
