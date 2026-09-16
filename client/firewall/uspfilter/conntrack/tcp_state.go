package conntrack

import nftypes "github.com/openzro/openzro/client/internal/netflow/types"

// nextState returns the state a connection moves to on a segment, or 0 when
// the segment does not move it. connDir is the side that sent the SYN;
// packetDir is the side that sent this segment. The teardown rules only
// advance on the side each state is waiting on: a duplicate FIN or ACK from
// the other side is valid traffic, but it is not the event the state
// machine is waiting for, and treating it as one closes flows early.
func nextState(current TCPState, connDir, packetDir nftypes.Direction, flags uint8) TCPState {
	same := packetDir == connDir
	syn, ack, fin := flags&TCPSyn != 0, flags&TCPAck != 0, flags&TCPFin != 0

	switch current {
	case TCPStateNew:
		if syn && !ack {
			if connDir == nftypes.Egress {
				return TCPStateSynSent
			}
			return TCPStateSynReceived
		}

	case TCPStateSynSent:
		if syn && ack {
			if same {
				return TCPStateSynReceived // simultaneous open
			}
			return TCPStateEstablished
		}

	case TCPStateSynReceived:
		if ack && !syn && same {
			return TCPStateEstablished
		}

	case TCPStateEstablished:
		if fin {
			if same {
				return TCPStateFinWait1
			}
			return TCPStateCloseWait
		}

	case TCPStateFinWait1:
		// Only the peer can move us on. FIN+ACK acknowledges our FIN and
		// carries theirs, so we skip Closing (RFC 9293 3.10.7.4); a lone
		// FIN is a simultaneous close; a lone ACK means wait for their FIN.
		if same {
			return 0
		}
		switch {
		case fin && ack:
			return TCPStateTimeWait
		case fin:
			return TCPStateClosing
		case ack:
			return TCPStateFinWait2
		}

	case TCPStateFinWait2:
		if fin && !same {
			return TCPStateTimeWait
		}

	case TCPStateClosing:
		if ack && !same {
			return TCPStateTimeWait
		}

	case TCPStateCloseWait:
		if fin && same {
			return TCPStateLastAck
		}

	case TCPStateLastAck:
		if ack && !same {
			return TCPStateClosed
		}
	}
	return 0
}

// isValidStateForFlags checks if the TCP flags are valid for the current connection state
func (t *TCPTracker) isValidStateForFlags(state TCPState, flags uint8) bool {
	if !isValidFlagCombination(flags) {
		return false
	}
	if flags&TCPRst != 0 {
		if state == TCPStateSynSent {
			return flags&TCPAck != 0
		}
		return true
	}

	switch state {
	case TCPStateNew:
		return flags&TCPSyn != 0 && flags&TCPAck == 0
	case TCPStateSynSent:
		// TODO: support simultaneous open
		return flags&TCPSyn != 0 && flags&TCPAck != 0
	case TCPStateSynReceived:
		return flags&TCPAck != 0
	case TCPStateEstablished:
		return flags&TCPAck != 0
	case TCPStateFinWait1:
		return flags&TCPFin != 0 || flags&TCPAck != 0
	case TCPStateFinWait2:
		return flags&TCPFin != 0 || flags&TCPAck != 0
	case TCPStateClosing:
		// In CLOSING state, we should accept the final ACK
		return flags&TCPAck != 0
	case TCPStateTimeWait:
		// In TIME_WAIT, we might see retransmissions
		return flags&TCPAck != 0
	case TCPStateCloseWait:
		return flags&TCPFin != 0 || flags&TCPAck != 0
	case TCPStateLastAck:
		return flags&TCPAck != 0
	case TCPStateClosed:
		// Accept retransmitted ACKs in closed state, the final ACK might be lost and the peer will retransmit their FIN-ACK
		return flags&TCPAck != 0
	}
	return false
}

func isValidFlagCombination(flags uint8) bool {
	// Invalid: SYN+FIN
	if flags&TCPSyn != 0 && flags&TCPFin != 0 {
		return false
	}

	// Invalid: RST with SYN or FIN
	if flags&TCPRst != 0 && (flags&TCPSyn != 0 || flags&TCPFin != 0) {
		return false
	}

	return true
}
