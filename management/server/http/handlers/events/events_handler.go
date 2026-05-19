package events

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/management/server/account"
	"github.com/openzro/openzro/management/server/activity"
	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/http/api"
	"github.com/openzro/openzro/management/server/http/util"
)

// handler HTTP handler
type handler struct {
	accountManager account.Manager
}

func AddEndpoints(accountManager account.Manager, router *mux.Router) {
	eventsHandler := newHandler(accountManager)
	router.HandleFunc("/events", eventsHandler.getAllEvents).Methods("GET", "OPTIONS")
	router.HandleFunc("/events/audit", eventsHandler.getAllEvents).Methods("GET", "OPTIONS")

	// Admission audit export. The endpoint exists because regulated
	// tenants (Bacen 4.893 / Circular 3.909) need a portable artifact
	// they can hand the auditor that proves "every non-compliant
	// device that tried to connect was refused, with timestamp and
	// reason." The dashboard's Activity tab covers day-to-day review;
	// this endpoint is for the quarterly / annual evidence package.
	router.HandleFunc("/events/admission.csv", eventsHandler.getAdmissionCSV).Methods("GET", "OPTIONS")
}

// newHandler creates a new events handler
func newHandler(accountManager account.Manager) *handler {
	return &handler{accountManager: accountManager}
}

// getAllEvents list of the given account
func (h *handler) getAllEvents(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		log.WithContext(r.Context()).Error(err)
		http.Redirect(w, r, "/", http.StatusInternalServerError)
		return
	}

	accountID, userID := userAuth.AccountId, userAuth.UserId

	accountEvents, err := h.accountManager.GetEvents(r.Context(), accountID, userID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	events := make([]*api.Event, len(accountEvents))
	for i, e := range accountEvents {
		events[i] = toEventResponse(e)
	}

	util.WriteJSONObject(r.Context(), w, events)
}

// getAdmissionCSV streams the admission-related slice of the audit
// log as CSV. Filters in-memory because (a) the upstream Get already
// caps at 10k events which is plenty for an admission report — denials
// are rare events by design — and (b) keeping the filter at the
// handler level avoids growing the activity.Store interface for a
// single use case.
//
// Query params:
//
//	from=RFC3339    inclusive lower bound on event timestamp (optional)
//	to=RFC3339      inclusive upper bound on event timestamp (optional)
//
// Output columns are stable and documented; auditors and SIEM ingest
// pipelines key off them.
func (h *handler) getAdmissionCSV(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		log.WithContext(r.Context()).Error(err)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	accountID, userID := userAuth.AccountId, userAuth.UserId

	from, to, err := parseTimeRange(r)
	if err != nil {
		util.WriteErrorResponse(err.Error(), http.StatusBadRequest, w)
		return
	}

	accountEvents, err := h.accountManager.GetEvents(r.Context(), accountID, userID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="openzro-admission-audit.csv"`)

	writer := csv.NewWriter(w)
	defer writer.Flush()

	_ = writer.Write([]string{
		"timestamp",
		"activity_code",
		"activity",
		"initiator_id",
		"initiator_name",
		"initiator_email",
		"target_id",
		"posture_check_id",
		"posture_check_name",
		"check_type",
		"reason",
		"peer_hostname",
	})

	for _, e := range accountEvents {
		if !isAdmissionEvent(e.Activity) {
			continue
		}
		if !from.IsZero() && e.Timestamp.Before(from) {
			continue
		}
		if !to.IsZero() && e.Timestamp.After(to) {
			continue
		}
		_ = writer.Write([]string{
			e.Timestamp.UTC().Format(time.RFC3339),
			e.Activity.StringCode(),
			e.Activity.Message(),
			e.InitiatorID,
			e.InitiatorName,
			e.InitiatorEmail,
			e.TargetID,
			metaString(e.Meta, "posture_check_id"),
			metaString(e.Meta, "posture_check_name"),
			metaString(e.Meta, "check_type"),
			metaString(e.Meta, "reason"),
			metaString(e.Meta, "peer_hostname"),
		})
	}
}

func isAdmissionEvent(a activity.Activity) bool {
	switch a {
	case activity.PeerAdmissionDenied,
		activity.AdmissionEnforcementEnabled,
		activity.AdmissionEnforcementDisabled,
		activity.AdmissionPostureChecksUpdated:
		return true
	}
	return false
}

func metaString(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	v, ok := meta[key]
	if !ok || v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

func parseTimeRange(r *http.Request) (time.Time, time.Time, error) {
	var from, to time.Time
	if v := r.URL.Query().Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("from: must be RFC3339, got %q", v)
		}
		from = t
	}
	if v := r.URL.Query().Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("to: must be RFC3339, got %q", v)
		}
		to = t
	}
	if !from.IsZero() && !to.IsZero() && from.After(to) {
		return time.Time{}, time.Time{}, fmt.Errorf("from must be <= to")
	}
	return from, to, nil
}

func toEventResponse(event *activity.Event) *api.Event {
	meta := make(map[string]string)
	if event.Meta != nil {
		for s, a := range event.Meta {
			meta[s] = fmt.Sprintf("%v", a)
		}
	}
	e := &api.Event{
		Id:             fmt.Sprint(event.ID),
		InitiatorId:    event.InitiatorID,
		InitiatorName:  event.InitiatorName,
		InitiatorEmail: event.InitiatorEmail,
		Activity:       event.Activity.Message(),
		ActivityCode:   api.EventActivityCode(event.Activity.StringCode()),
		TargetId:       event.TargetID,
		Timestamp:      event.Timestamp,
		Meta:           meta,
	}
	return e
}
