// Custom DNS Zones HTTP handlers (issue #108, ADR-0022 Phase 1).
//
// License posture per ADR-0022 D8: AGPL clean-room. Mirrors the
// shape of nameservers_handler.go (BSD nameserver-group surface) +
// the public NetBird OpenAPI documented at
// docs.netbird.io/api/resources/dns-zones. No upstream AGPL diff
// consulted.
package dns

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/management/server/account"
	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/http/api"
	"github.com/openzro/openzro/management/server/http/util"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/types"
)

type zonesHandler struct {
	accountManager account.Manager
}

// addDNSZonesEndpoint wires the 10 zone/record CRUD endpoints onto
// the router. Mirrors addDNSNameserversEndpoint at
// nameservers_handler.go:24.
func addDNSZonesEndpoint(accountManager account.Manager, router *mux.Router) {
	h := &zonesHandler{accountManager: accountManager}
	router.HandleFunc("/dns/zones", h.listZones).Methods("GET", "OPTIONS")
	router.HandleFunc("/dns/zones", h.createZone).Methods("POST", "OPTIONS")
	router.HandleFunc("/dns/zones/{zoneId}", h.getZone).Methods("GET", "OPTIONS")
	router.HandleFunc("/dns/zones/{zoneId}", h.updateZone).Methods("PUT", "OPTIONS")
	router.HandleFunc("/dns/zones/{zoneId}", h.deleteZone).Methods("DELETE", "OPTIONS")
	router.HandleFunc("/dns/zones/{zoneId}/records", h.listRecords).Methods("GET", "OPTIONS")
	router.HandleFunc("/dns/zones/{zoneId}/records", h.createRecord).Methods("POST", "OPTIONS")
	router.HandleFunc("/dns/zones/{zoneId}/records/{recordId}", h.getRecord).Methods("GET", "OPTIONS")
	router.HandleFunc("/dns/zones/{zoneId}/records/{recordId}", h.updateRecord).Methods("PUT", "OPTIONS")
	router.HandleFunc("/dns/zones/{zoneId}/records/{recordId}", h.deleteRecord).Methods("DELETE", "OPTIONS")
}

// -- Zone endpoints ----------------------------------------------------

func (h *zonesHandler) listZones(w http.ResponseWriter, r *http.Request) {
	accountID, userID, ok := authContext(w, r)
	if !ok {
		return
	}
	zones, err := h.accountManager.ListDNSZones(r.Context(), accountID, userID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	out := make([]*api.DNSZone, 0, len(zones))
	for _, z := range zones {
		out = append(out, toAPIZone(z))
	}
	util.WriteJSONObject(r.Context(), w, out)
}

func (h *zonesHandler) createZone(w http.ResponseWriter, r *http.Request) {
	accountID, userID, ok := authContext(w, r)
	if !ok {
		return
	}
	var req api.PostApiDnsZonesJSONRequestBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		util.WriteErrorResponse("couldn't parse JSON request", http.StatusBadRequest, w)
		return
	}
	zone := fromAPIZoneRequest(req)
	saved, err := h.accountManager.CreateDNSZone(r.Context(), accountID, userID, zone)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, toAPIZone(saved))
}

func (h *zonesHandler) getZone(w http.ResponseWriter, r *http.Request) {
	accountID, userID, ok := authContext(w, r)
	if !ok {
		return
	}
	zoneID := mux.Vars(r)["zoneId"]
	if zoneID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "invalid zone id"), w)
		return
	}
	zone, err := h.accountManager.GetDNSZone(r.Context(), accountID, userID, zoneID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, toAPIZone(zone))
}

func (h *zonesHandler) updateZone(w http.ResponseWriter, r *http.Request) {
	accountID, userID, ok := authContext(w, r)
	if !ok {
		return
	}
	zoneID := mux.Vars(r)["zoneId"]
	if zoneID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "invalid zone id"), w)
		return
	}
	var req api.PutApiDnsZonesZoneIdJSONRequestBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		util.WriteErrorResponse("couldn't parse JSON request", http.StatusBadRequest, w)
		return
	}
	zone := fromAPIZoneRequest(req)
	zone.ID = zoneID
	saved, err := h.accountManager.SaveDNSZone(r.Context(), accountID, userID, zone)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, toAPIZone(saved))
}

func (h *zonesHandler) deleteZone(w http.ResponseWriter, r *http.Request) {
	accountID, userID, ok := authContext(w, r)
	if !ok {
		return
	}
	zoneID := mux.Vars(r)["zoneId"]
	if zoneID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "invalid zone id"), w)
		return
	}
	if err := h.accountManager.DeleteDNSZone(r.Context(), accountID, zoneID, userID); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, emptyObject{})
}

// -- Record endpoints --------------------------------------------------

func (h *zonesHandler) listRecords(w http.ResponseWriter, r *http.Request) {
	accountID, userID, ok := authContext(w, r)
	if !ok {
		return
	}
	zoneID := mux.Vars(r)["zoneId"]
	if zoneID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "invalid zone id"), w)
		return
	}
	records, err := h.accountManager.ListDNSRecords(r.Context(), accountID, userID, zoneID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	out := make([]*api.DNSRecord, 0, len(records))
	for _, rec := range records {
		out = append(out, toAPIRecord(rec))
	}
	util.WriteJSONObject(r.Context(), w, out)
}

func (h *zonesHandler) createRecord(w http.ResponseWriter, r *http.Request) {
	accountID, userID, ok := authContext(w, r)
	if !ok {
		return
	}
	zoneID := mux.Vars(r)["zoneId"]
	if zoneID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "invalid zone id"), w)
		return
	}
	var req api.PostApiDnsZonesZoneIdRecordsJSONRequestBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		util.WriteErrorResponse("couldn't parse JSON request", http.StatusBadRequest, w)
		return
	}
	if err := validateRecordTTLAtAPI(req.Ttl); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	saved, err := h.accountManager.CreateDNSRecord(r.Context(), accountID, userID, zoneID, fromAPIRecordRequest(req))
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, toAPIRecord(saved))
}

func (h *zonesHandler) getRecord(w http.ResponseWriter, r *http.Request) {
	accountID, userID, ok := authContext(w, r)
	if !ok {
		return
	}
	zoneID, recordID := mux.Vars(r)["zoneId"], mux.Vars(r)["recordId"]
	if zoneID == "" || recordID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "invalid zone or record id"), w)
		return
	}
	rec, err := h.accountManager.GetDNSRecord(r.Context(), accountID, userID, zoneID, recordID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, toAPIRecord(rec))
}

func (h *zonesHandler) updateRecord(w http.ResponseWriter, r *http.Request) {
	accountID, userID, ok := authContext(w, r)
	if !ok {
		return
	}
	zoneID, recordID := mux.Vars(r)["zoneId"], mux.Vars(r)["recordId"]
	if zoneID == "" || recordID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "invalid zone or record id"), w)
		return
	}
	var req api.PutApiDnsZonesZoneIdRecordsRecordIdJSONRequestBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		util.WriteErrorResponse("couldn't parse JSON request", http.StatusBadRequest, w)
		return
	}
	if err := validateRecordTTLAtAPI(req.Ttl); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	record := fromAPIRecordRequest(req)
	record.ID = recordID
	saved, err := h.accountManager.SaveDNSRecord(r.Context(), accountID, userID, zoneID, record)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, toAPIRecord(saved))
}

func (h *zonesHandler) deleteRecord(w http.ResponseWriter, r *http.Request) {
	accountID, userID, ok := authContext(w, r)
	if !ok {
		return
	}
	zoneID, recordID := mux.Vars(r)["zoneId"], mux.Vars(r)["recordId"]
	if zoneID == "" || recordID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "invalid zone or record id"), w)
		return
	}
	if err := h.accountManager.DeleteDNSRecord(r.Context(), accountID, zoneID, recordID, userID); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, emptyObject{})
}

// -- API ↔ domain converters -------------------------------------------

func toAPIZone(z *types.DNSZone) *api.DNSZone {
	if z == nil {
		return nil
	}
	enabled := z.Enabled
	searchDomain := z.SearchDomainEnabled
	records := make([]api.DNSRecord, 0, len(z.Records))
	for i := range z.Records {
		records = append(records, *toAPIRecord(&z.Records[i]))
	}
	return &api.DNSZone{
		Id:                 z.ID,
		Name:               z.Name,
		Domain:             z.Domain,
		Enabled:            &enabled,
		EnableSearchDomain: &searchDomain,
		DistributionGroups: z.GroupIDs(),
		Records:            records,
	}
}

func toAPIRecord(r *types.DNSRecord) *api.DNSRecord {
	if r == nil {
		return nil
	}
	ttl := r.TTL
	return &api.DNSRecord{
		Id:      r.ID,
		Name:    r.Name,
		Type:    api.DNSRecordType(r.Type),
		Content: r.Content,
		Ttl:     &ttl,
	}
}

func fromAPIZoneRequest(req api.DNSZoneRequest) *types.DNSZone {
	zone := &types.DNSZone{
		Name:   req.Name,
		Domain: req.Domain,
	}
	if req.Enabled != nil {
		zone.Enabled = *req.Enabled
	} else {
		// Default enabled when the field is omitted — matches the
		// OpenAPI `default: true` declaration. Without this, a POST
		// without `enabled` would create a non-distributed zone, which
		// is not what the client requested.
		zone.Enabled = true
	}
	if req.EnableSearchDomain != nil {
		zone.SearchDomainEnabled = *req.EnableSearchDomain
	}
	zone.DistributionGroups = make([]types.DNSZoneGroup, 0, len(req.DistributionGroups))
	for _, gid := range req.DistributionGroups {
		zone.DistributionGroups = append(zone.DistributionGroups, types.DNSZoneGroup{GroupID: gid})
	}
	return zone
}

func fromAPIRecordRequest(req api.DNSRecordRequest) *types.DNSRecord {
	record := &types.DNSRecord{
		Name:    req.Name,
		Type:    string(req.Type),
		Content: req.Content,
	}
	if req.Ttl != nil {
		record.TTL = *req.Ttl
	}
	return record
}

// -- shared helpers ----------------------------------------------------

// emptyObject is returned by 200 OK on DELETE — JSON `{}`, matching
// the rest of the API's delete responses.
type emptyObject struct{}

// validateRecordTTLAtAPI enforces the OpenAPI `ttl: minimum: 1`
// contract at the wire boundary. The manager layer also defaults
// `TTL <= 0` to 300 (defense-in-depth for non-API callers / tests),
// but that path is unreachable for HTTP traffic when this check runs
// first: an omitted `ttl` field arrives as `req.Ttl == nil` and stays
// nil through the converter, so the manager defaults to 300; an
// explicit `ttl: 0` or `ttl: -1` from the wire is rejected here as
// 400 InvalidArgument instead of being silently rewritten.
//
// Without this check (Phase 1 review finding #2), the OpenAPI promise
// of `minimum: 1` was a lie — the JSON decoder happily filled `*int`
// with `0` and the manager normalized it to 300. Operators who set
// `ttl: 0` expecting "no cache" got 300 with no error.
func validateRecordTTLAtAPI(ttl *int) error {
	if ttl == nil {
		return nil // omitted → manager defaults to 300
	}
	if *ttl < 1 {
		return status.Errorf(status.InvalidArgument,
			"dns record ttl must be ≥ 1 (got %d); omit the field to use the default (300)", *ttl)
	}
	return nil
}

// authContext extracts (accountID, userID) from the request. On any
// failure path it writes the appropriate error response and returns
// ok=false so the caller can return immediately.
func authContext(w http.ResponseWriter, r *http.Request) (accountID, userID string, ok bool) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		log.WithContext(r.Context()).Error(err)
		util.WriteError(r.Context(), err, w)
		return "", "", false
	}
	return userAuth.AccountId, userAuth.UserId, true
}
