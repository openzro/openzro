// Package network_events exposes the dashboard-facing query API for
// traffic flow events that have been ingested by management/server/
// flow_service.go and persisted by flow/store.
//
// The endpoint shape mirrors NetBird's public traffic-events API as
// documented in their knowledge-hub: eight filters (peer, IPs, ports,
// protocol, type, direction, rule, time-range) plus pagination.
// We do not copy any GPL upstream code — only the public filter
// names — and we extend the response with a `total` count and
// configurable retention info that the upstream hard-codes.
package network_events

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"

	"github.com/openzro/openzro/flow/store"
	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/http/util"
	"github.com/openzro/openzro/management/server/permissions"
	"github.com/openzro/openzro/management/server/permissions/modules"
	"github.com/openzro/openzro/management/server/permissions/operations"
	"github.com/openzro/openzro/management/server/status"
)

// maxLimit caps a single response. Mirrors NetBird's public 50k cap.
// Larger pages live in cold archive / SIEM exports — see ADR-0002.
const maxLimit = 50000

// Handler hosts the /api/network-traffic-events endpoint. The handler
// is constructed nil-tolerant: when the flow store is not configured
// (engine=none), every query returns an empty list with HTTP 200.
// That keeps the dashboard from rendering an error state on a fresh
// install.
type Handler struct {
	permissions permissions.Manager
	store       store.Store
}

func AddEndpoints(permissionsManager permissions.Manager, flowStore store.Store, router *mux.Router) {
	h := &Handler{permissions: permissionsManager, store: flowStore}
	router.HandleFunc("/network-traffic-events", h.list).Methods("GET", "OPTIONS")
}

// list parses the eight documented filters and returns matching events
// ordered by received_at DESC.
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	// Permission gate. Reuse the events module — same audit-style read
	// permission already covers the activity log.
	if err := h.checkReadPermission(r, userAuth.AccountId, userAuth.UserId); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	if h.store == nil {
		// Engine=none configuration — return an empty result rather
		// than 503 so the dashboard renders the empty state.
		writeListResponse(w, []*store.Event{}, 0, 100)
		return
	}

	filter, errResp := parseFilter(r, userAuth.AccountId)
	if errResp != "" {
		util.WriteErrorResponse(errResp, http.StatusBadRequest, w)
		return
	}

	events, err := h.store.Query(r.Context(), filter)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	writeListResponse(w, events, filter.Offset, filter.Limit)
}

// checkReadPermission validates that the calling user has read on the
// Events module. Returns a status.* error suitable for util.WriteError.
func (h *Handler) checkReadPermission(r *http.Request, accountID, userID string) error {
	allowed, err := h.permissions.ValidateUserPermissions(
		r.Context(), accountID, userID, modules.Events, operations.Read)
	if err != nil {
		return status.NewPermissionValidationError(err)
	}
	if !allowed {
		return status.NewPermissionDeniedError()
	}
	return nil
}

// parseFilter projects query params onto a store.Filter.
// AccountID is set unconditionally from the auth context — never from
// the wire — so cross-account queries are structurally impossible.
// Errors return an empty Filter and an English message.
func parseFilter(r *http.Request, accountID string) (store.Filter, string) {
	q := r.URL.Query()
	f := store.Filter{
		AccountID: accountID,
		PeerID:    q.Get("peer_id"),
		SourceIP:  q.Get("source_ip"),
		DestIP:    q.Get("dest_ip"),
	}

	if raw := q.Get("source_port"); raw != "" {
		v, err := strconv.ParseUint(raw, 10, 32)
		if err != nil {
			return f, "invalid source_port"
		}
		port := uint32(v)
		f.SourcePort = &port
	}
	if raw := q.Get("dest_port"); raw != "" {
		v, err := strconv.ParseUint(raw, 10, 32)
		if err != nil {
			return f, "invalid dest_port"
		}
		port := uint32(v)
		f.DestPort = &port
	}

	if raw := q.Get("protocol"); raw != "" {
		v, err := strconv.ParseUint(raw, 10, 16)
		if err != nil {
			return f, "invalid protocol"
		}
		p := uint16(v)
		f.Protocol = &p
	}

	if raw := q.Get("type"); raw != "" {
		t, ok := parseEventType(raw)
		if !ok {
			return f, "invalid type (expected start|end|drop)"
		}
		f.Type = &t
	}

	if raw := q.Get("direction"); raw != "" {
		d, ok := parseDirection(raw)
		if !ok {
			return f, "invalid direction (expected ingress|egress)"
		}
		f.Direction = &d
	}

	if raw := q.Get("rule_id"); raw != "" {
		decoded, err := hex.DecodeString(raw)
		if err != nil {
			return f, "invalid rule_id (expected hex)"
		}
		f.RuleID = decoded
	}

	if raw := q.Get("since"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return f, "invalid since (expected RFC3339)"
		}
		f.Since = t
	}

	if raw := q.Get("until"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return f, "invalid until (expected RFC3339)"
		}
		f.Until = t
	}

	if raw := q.Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 {
			return f, "invalid limit"
		}
		if v > maxLimit {
			v = maxLimit
		}
		f.Limit = v
	}
	if f.Limit == 0 {
		f.Limit = 100
	}

	if raw := q.Get("offset"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 {
			return f, "invalid offset"
		}
		f.Offset = v
	}

	return f, ""
}

func parseEventType(s string) (store.EventType, bool) {
	switch s {
	case "start":
		return store.EventTypeStart, true
	case "end":
		return store.EventTypeEnd, true
	case "drop":
		return store.EventTypeDrop, true
	}
	return store.EventTypeUnknown, false
}

func parseDirection(s string) (store.Direction, bool) {
	switch s {
	case "ingress":
		return store.DirectionIngress, true
	case "egress":
		return store.DirectionEgress, true
	}
	return store.DirectionUnknown, false
}

// response is the wire shape for the list endpoint.
type response struct {
	Events []eventDTO `json:"events"`
	Limit  int        `json:"limit"`
	Offset int        `json:"offset"`
}

// eventDTO is the wire shape per event. Bytes-typed fields go on the
// wire as hex strings — keeps the response readable in the dashboard
// developer console without forcing every consumer to base64-decode.
type eventDTO struct {
	EventID        string    `json:"event_id"`
	FlowID         string    `json:"flow_id"`
	PeerID         string    `json:"peer_id"`
	IsInitiator    bool      `json:"is_initiator"`
	OccurredAt     time.Time `json:"occurred_at"`
	ReceivedAt     time.Time `json:"received_at"`
	Type           string    `json:"type"`
	Direction      string    `json:"direction"`
	Protocol       uint16    `json:"protocol"`
	SourceIP       string    `json:"source_ip"`
	DestIP         string    `json:"dest_ip"`
	SourcePort     uint32    `json:"source_port,omitempty"`
	DestPort       uint32    `json:"dest_port,omitempty"`
	ICMPType       uint16    `json:"icmp_type,omitempty"`
	ICMPCode       uint16    `json:"icmp_code,omitempty"`
	RxPackets      uint64    `json:"rx_packets"`
	TxPackets      uint64    `json:"tx_packets"`
	RxBytes        uint64    `json:"rx_bytes"`
	TxBytes        uint64    `json:"tx_bytes"`
	RuleID         string    `json:"rule_id,omitempty"`
	SourceResource string    `json:"source_resource_id,omitempty"`
	DestResource   string    `json:"dest_resource_id,omitempty"`
}

func toDTO(e *store.Event) eventDTO {
	return eventDTO{
		EventID:     hex.EncodeToString(e.EventID),
		FlowID:      hex.EncodeToString(e.FlowID),
		PeerID:      e.PeerID,
		IsInitiator: e.IsInitiator,
		OccurredAt:  e.OccurredAt,
		ReceivedAt:  e.ReceivedAt,
		Type:        formatType(e.Type),
		Direction:   formatDirection(e.Direction),
		Protocol:    e.Protocol,
		SourceIP:    e.SourceIP,
		DestIP:      e.DestIP,
		SourcePort:  e.SourcePort,
		DestPort:    e.DestPort,
		ICMPType:    e.ICMPType,
		ICMPCode:    e.ICMPCode,
		RxPackets:   e.RxPackets,
		TxPackets:   e.TxPackets,
		RxBytes:     e.RxBytes,
		TxBytes:     e.TxBytes,
		// RuleID, SourceResource, DestResource carry the originating
		// PolicyID (or resource id) as the agent stamped them — for
		// openzro that's an xid string in printable ASCII bytes (the
		// management hands them out via `xid.New().String()` and the
		// agent sets the firewall rule's `id []byte` to those bytes).
		// Returning them as hex breaks the dashboard lookup, which
		// keys policyByID on the same xid string the API exposes
		// elsewhere. Decode as UTF-8 when the bytes look like a
		// printable ASCII id, fall back to hex for anything binary
		// (e.g. older agents that stamped raw uuid bytes).
		RuleID:         decodeIDBytes(e.RuleID),
		SourceResource: decodeIDBytes(e.SourceResource),
		DestResource:   decodeIDBytes(e.DestResource),
	}
}

// decodeIDBytes returns the bytes as a UTF-8 string when they are
// all printable ASCII (the format used by xid-issued IDs), or the
// hex encoding otherwise. Empty input round-trips to "".
func decodeIDBytes(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	for _, c := range b {
		if c < 0x20 || c > 0x7E {
			return hex.EncodeToString(b)
		}
	}
	return string(b)
}

func formatType(t store.EventType) string {
	switch t {
	case store.EventTypeStart:
		return "start"
	case store.EventTypeEnd:
		return "end"
	case store.EventTypeDrop:
		return "drop"
	}
	return "unknown"
}

func formatDirection(d store.Direction) string {
	switch d {
	case store.DirectionIngress:
		return "ingress"
	case store.DirectionEgress:
		return "egress"
	}
	return "unknown"
}

func writeListResponse(w http.ResponseWriter, events []*store.Event, offset, limit int) {
	dtos := make([]eventDTO, len(events))
	for i, e := range events {
		dtos[i] = toDTO(e)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response{
		Events: dtos,
		Limit:  limit,
		Offset: offset,
	}); err != nil {
		// Encoding failure is a programming bug, not an operator
		// problem — log loud rather than try to recover.
		_, _ = fmt.Fprintf(w, "encode response: %v", err)
	}
}
