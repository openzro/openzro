package peers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/management/server/account"
	"github.com/openzro/openzro/management/server/activity"
	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/groups"
	"github.com/openzro/openzro/management/server/http/api"
	"github.com/openzro/openzro/management/server/http/util"
	nbpeer "github.com/openzro/openzro/management/server/peer"
	"github.com/openzro/openzro/management/server/posture"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/types"
)

// Handler is a handler that returns peers of the account
type Handler struct {
	accountManager   account.Manager
	postureEvalStore posture.EvalStore
}

func AddEndpoints(accountManager account.Manager, postureEvalStore posture.EvalStore, router *mux.Router) {
	peersHandler := NewHandler(accountManager, postureEvalStore)
	router.HandleFunc("/peers", peersHandler.GetAllPeers).Methods("GET", "OPTIONS")
	router.HandleFunc("/peers/{peerId}", peersHandler.HandlePeer).
		Methods("GET", "PUT", "DELETE", "OPTIONS")
	router.HandleFunc("/peers/{peerId}/accessible-peers", peersHandler.GetAccessiblePeers).Methods("GET", "OPTIONS")
	// Per-peer posture evaluation timeline (#38). Returns the most
	// recent N rows from posture_evaluations for the peer, newest
	// first. Admin-only at the handler level; regular users get
	// 403 since the data covers ALL posture checks across all
	// policies the peer might be subject to.
	router.HandleFunc("/peers/{peerId}/posture-evaluations", peersHandler.GetPostureEvaluations).Methods("GET", "OPTIONS")
}

// NewHandler creates a new peers Handler
func NewHandler(accountManager account.Manager, postureEvalStore posture.EvalStore) *Handler {
	return &Handler{
		accountManager:   accountManager,
		postureEvalStore: postureEvalStore,
	}
}

func (h *Handler) checkPeerStatus(peer *nbpeer.Peer) (*nbpeer.Peer, error) {
	// The store-side Status.Connected is authoritative: it's set true
	// in updatePeerStatusAndLocation when the peer Sync stream attaches
	// to any replica, and false in peer/peer.go when the stream closes
	// or detection-stale fires.
	//
	// Earlier versions also overrode Connected to false here when
	// HasConnectedChannel reported no local channel — a single-replica
	// safety net for "server restarted but peer hasn't reconnected yet"
	// gap. That override breaks HA: HasConnectedChannel only sees
	// channels owned by THIS replica's local map, so in an N-replica
	// fleet roughly (N-1)/N of /api/peers requests land on a replica
	// that doesn't own the stream and report every healthy peer as
	// disconnected. The dashboard then flickers each refresh as the
	// load balancer cycles through replicas. Trust the store; accept
	// the ~keep-alive-timeout window of "ghost connected" that may
	// follow an abrupt pod kill — peers reconnect within that window
	// and the store catches up.
	return peer.Copy(), nil
}

func (h *Handler) getPeer(ctx context.Context, accountID, peerID, userID string, w http.ResponseWriter) {
	peer, err := h.accountManager.GetPeer(ctx, accountID, peerID, userID)
	if err != nil {
		util.WriteError(ctx, err, w)
		return
	}

	peerToReturn, err := h.checkPeerStatus(peer)
	if err != nil {
		util.WriteError(ctx, err, w)
		return
	}
	settings, err := h.accountManager.GetAccountSettings(ctx, accountID, activity.SystemInitiator)
	if err != nil {
		util.WriteError(ctx, err, w)
		return
	}

	dnsDomain := h.accountManager.GetDNSDomain(settings)

	grps, _ := h.accountManager.GetPeerGroups(ctx, accountID, peerID)
	grpsInfoMap := groups.ToGroupsInfoMap(grps, 0)

	validPeers, err := h.accountManager.GetValidatedPeers(ctx, accountID)
	if err != nil {
		log.WithContext(ctx).Errorf("failed to list approved peers: %v", err)
		util.WriteError(ctx, fmt.Errorf("internal error"), w)
		return
	}

	_, valid := validPeers[peer.ID]
	util.WriteJSONObject(ctx, w, toSinglePeerResponse(peerToReturn, grpsInfoMap[peerID], dnsDomain, valid))
}

func (h *Handler) updatePeer(ctx context.Context, accountID, userID, peerID string, w http.ResponseWriter, r *http.Request) {
	req := &api.PeerRequest{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		util.WriteErrorResponse("couldn't parse JSON request", http.StatusBadRequest, w)
		return
	}

	update := &nbpeer.Peer{
		ID:                     peerID,
		SSHEnabled:             req.SshEnabled,
		Name:                   req.Name,
		LoginExpirationEnabled: req.LoginExpirationEnabled,

		InactivityExpirationEnabled: req.InactivityExpirationEnabled,
	}

	if req.ApprovalRequired != nil {
		// todo: looks like that we reset all status property, is it right?
		update.Status = &nbpeer.PeerStatus{
			RequiresApproval: *req.ApprovalRequired,
		}
	}

	peer, err := h.accountManager.UpdatePeer(ctx, accountID, userID, update)
	if err != nil {
		util.WriteError(ctx, err, w)
		return
	}

	settings, err := h.accountManager.GetAccountSettings(ctx, accountID, activity.SystemInitiator)
	if err != nil {
		util.WriteError(ctx, err, w)
		return
	}
	dnsDomain := h.accountManager.GetDNSDomain(settings)

	peerGroups, err := h.accountManager.GetPeerGroups(ctx, accountID, peer.ID)
	if err != nil {
		util.WriteError(ctx, err, w)
		return
	}

	grpsInfoMap := groups.ToGroupsInfoMap(peerGroups, 0)

	validPeers, err := h.accountManager.GetValidatedPeers(ctx, accountID)
	if err != nil {
		log.WithContext(ctx).Errorf("failed to list appreoved peers: %v", err)
		util.WriteError(ctx, fmt.Errorf("internal error"), w)
		return
	}

	_, valid := validPeers[peer.ID]

	util.WriteJSONObject(r.Context(), w, toSinglePeerResponse(peer, grpsInfoMap[peerID], dnsDomain, valid))
}

func (h *Handler) deletePeer(ctx context.Context, accountID, userID string, peerID string, w http.ResponseWriter) {
	err := h.accountManager.DeletePeer(ctx, accountID, peerID, userID)
	if err != nil {
		log.WithContext(ctx).Errorf("failed to delete peer: %v", err)
		util.WriteError(ctx, err, w)
		return
	}
	util.WriteJSONObject(ctx, w, util.EmptyObject{})
}

// HandlePeer handles all peer requests for GET, PUT and DELETE operations
func (h *Handler) HandlePeer(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	accountID, userID := userAuth.AccountId, userAuth.UserId
	vars := mux.Vars(r)
	peerID := vars["peerId"]
	if len(peerID) == 0 {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "invalid peer ID"), w)
		return
	}

	switch r.Method {
	case http.MethodDelete:
		h.deletePeer(r.Context(), accountID, userID, peerID, w)
		return
	case http.MethodGet:
		h.getPeer(r.Context(), accountID, peerID, userID, w)
		return
	case http.MethodPut:
		h.updatePeer(r.Context(), accountID, userID, peerID, w, r)
		return
	default:
		util.WriteError(r.Context(), status.Errorf(status.NotFound, "unknown METHOD"), w)
	}
}

// GetAllPeers returns a list of all peers associated with a provided account
func (h *Handler) GetAllPeers(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	nameFilter := r.URL.Query().Get("name")
	ipFilter := r.URL.Query().Get("ip")

	accountID, userID := userAuth.AccountId, userAuth.UserId

	peers, err := h.accountManager.GetPeers(r.Context(), accountID, userID, nameFilter, ipFilter)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	settings, err := h.accountManager.GetAccountSettings(r.Context(), accountID, activity.SystemInitiator)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	dnsDomain := h.accountManager.GetDNSDomain(settings)

	grps, _ := h.accountManager.GetAllGroups(r.Context(), accountID, userID)

	grpsInfoMap := groups.ToGroupsInfoMap(grps, len(peers))
	respBody := make([]*api.PeerBatch, 0, len(peers))
	for _, peer := range peers {
		peerToReturn, err := h.checkPeerStatus(peer)
		if err != nil {
			util.WriteError(r.Context(), err, w)
			return
		}

		respBody = append(respBody, toPeerListItemResponse(peerToReturn, grpsInfoMap[peer.ID], dnsDomain, 0))
	}

	validPeersMap, err := h.accountManager.GetValidatedPeers(r.Context(), accountID)
	if err != nil {
		log.WithContext(r.Context()).Errorf("failed to list appreoved peers: %v", err)
		util.WriteError(r.Context(), fmt.Errorf("internal error"), w)
		return
	}
	h.setApprovalRequiredFlag(respBody, validPeersMap)

	util.WriteJSONObject(r.Context(), w, respBody)
}

func (h *Handler) setApprovalRequiredFlag(respBody []*api.PeerBatch, approvedPeersMap map[string]struct{}) {
	for _, peer := range respBody {
		_, ok := approvedPeersMap[peer.Id]
		if !ok {
			peer.ApprovalRequired = true
		}
	}
}

// GetAccessiblePeers returns a list of all peers that the specified peer can connect to within the network.
func (h *Handler) GetAccessiblePeers(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	accountID, userID := userAuth.AccountId, userAuth.UserId

	vars := mux.Vars(r)
	peerID := vars["peerId"]
	if len(peerID) == 0 {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "invalid peer ID"), w)
		return
	}

	account, err := h.accountManager.GetAccountByID(r.Context(), accountID, activity.SystemInitiator)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	user, err := h.accountManager.GetUserByID(r.Context(), userID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	// If the user is regular user and does not own the peer
	// with the given peerID return an empty list
	if !user.HasAdminPower() && !user.IsServiceUser && !userAuth.IsChild {
		peer, ok := account.Peers[peerID]
		if !ok {
			util.WriteError(r.Context(), status.Errorf(status.NotFound, "peer not found"), w)
			return
		}

		if peer.UserID != user.Id {
			util.WriteJSONObject(r.Context(), w, []api.AccessiblePeer{})
			return
		}
	}

	validPeers, err := h.accountManager.GetValidatedPeers(r.Context(), accountID)
	if err != nil {
		log.WithContext(r.Context()).Errorf("failed to list approved peers: %v", err)
		util.WriteError(r.Context(), fmt.Errorf("internal error"), w)
		return
	}

	dnsDomain := h.accountManager.GetDNSDomain(account.Settings)

	customZone := account.GetPeersCustomZone(r.Context(), dnsDomain)
	netMap := account.GetPeerNetworkMap(r.Context(), peerID, customZone, validPeers, account.GetResourcePoliciesMap(), account.GetResourceRoutersMap(), nil)

	util.WriteJSONObject(r.Context(), w, toAccessiblePeers(netMap, dnsDomain))
}

func toAccessiblePeers(netMap *types.NetworkMap, dnsDomain string) []api.AccessiblePeer {
	accessiblePeers := make([]api.AccessiblePeer, 0, len(netMap.Peers)+len(netMap.OfflinePeers))
	for _, p := range netMap.Peers {
		accessiblePeers = append(accessiblePeers, peerToAccessiblePeer(p, dnsDomain))
	}

	for _, p := range netMap.OfflinePeers {
		accessiblePeers = append(accessiblePeers, peerToAccessiblePeer(p, dnsDomain))
	}

	return accessiblePeers
}

func peerToAccessiblePeer(peer *nbpeer.Peer, dnsDomain string) api.AccessiblePeer {
	return api.AccessiblePeer{
		CityName:    peer.Location.CityName,
		Connected:   peer.Status.Connected,
		CountryCode: peer.Location.CountryCode,
		DnsLabel:    fqdn(peer, dnsDomain),
		GeonameId:   int(peer.Location.GeoNameID),
		Id:          peer.ID,
		Ip:          peer.IP.String(),
		LastSeen:    peer.Status.LastSeen,
		Name:        peer.Name,
		Os:          peer.Meta.OS,
		UserId:      peer.UserID,
	}
}

func toSinglePeerResponse(peer *nbpeer.Peer, groupsInfo []api.GroupMinimum, dnsDomain string, approved bool) *api.Peer {
	osVersion := peer.Meta.OSVersion
	if osVersion == "" {
		osVersion = peer.Meta.Core
	}

	return &api.Peer{
		Id:                          peer.ID,
		Name:                        peer.Name,
		Ip:                          peer.IP.String(),
		ConnectionIp:                peer.Location.ConnectionIP.String(),
		Connected:                   peer.Status.Connected,
		LastSeen:                    peer.Status.LastSeen,
		Os:                          fmt.Sprintf("%s %s", peer.Meta.OS, osVersion),
		KernelVersion:               peer.Meta.KernelVersion,
		GeonameId:                   int(peer.Location.GeoNameID),
		Version:                     peer.Meta.WtVersion,
		Groups:                      groupsInfo,
		SshEnabled:                  peer.SSHEnabled,
		Hostname:                    peer.Meta.Hostname,
		UserId:                      peer.UserID,
		UiVersion:                   peer.Meta.UIVersion,
		DnsLabel:                    fqdn(peer, dnsDomain),
		ExtraDnsLabels:              fqdnList(peer.ExtraDNSLabels, dnsDomain),
		LoginExpirationEnabled:      peer.LoginExpirationEnabled,
		LastLogin:                   peer.GetLastLogin(),
		LoginExpired:                peer.Status.LoginExpired,
		ApprovalRequired:            !approved,
		CountryCode:                 peer.Location.CountryCode,
		CityName:                    peer.Location.CityName,
		SerialNumber:                peer.Meta.SystemSerialNumber,
		InactivityExpirationEnabled: peer.InactivityExpirationEnabled,
		Ephemeral:                   peer.Ephemeral,
	}
}

func toPeerListItemResponse(peer *nbpeer.Peer, groupsInfo []api.GroupMinimum, dnsDomain string, accessiblePeersCount int) *api.PeerBatch {
	osVersion := peer.Meta.OSVersion
	if osVersion == "" {
		osVersion = peer.Meta.Core
	}

	return &api.PeerBatch{
		Id:                     peer.ID,
		Name:                   peer.Name,
		Ip:                     peer.IP.String(),
		ConnectionIp:           peer.Location.ConnectionIP.String(),
		Connected:              peer.Status.Connected,
		LastSeen:               peer.Status.LastSeen,
		Os:                     fmt.Sprintf("%s %s", peer.Meta.OS, osVersion),
		KernelVersion:          peer.Meta.KernelVersion,
		GeonameId:              int(peer.Location.GeoNameID),
		Version:                peer.Meta.WtVersion,
		Groups:                 groupsInfo,
		SshEnabled:             peer.SSHEnabled,
		Hostname:               peer.Meta.Hostname,
		UserId:                 peer.UserID,
		UiVersion:              peer.Meta.UIVersion,
		DnsLabel:               fqdn(peer, dnsDomain),
		ExtraDnsLabels:         fqdnList(peer.ExtraDNSLabels, dnsDomain),
		LoginExpirationEnabled: peer.LoginExpirationEnabled,
		LastLogin:              peer.GetLastLogin(),
		LoginExpired:           peer.Status.LoginExpired,
		AccessiblePeersCount:   accessiblePeersCount,
		CountryCode:            peer.Location.CountryCode,
		CityName:               peer.Location.CityName,
		SerialNumber:           peer.Meta.SystemSerialNumber,

		InactivityExpirationEnabled: peer.InactivityExpirationEnabled,
	}
}

func fqdn(peer *nbpeer.Peer, dnsDomain string) string {
	fqdn := peer.FQDN(dnsDomain)
	if fqdn == "" {
		return peer.DNSLabel
	} else {
		return fqdn
	}
}
func fqdnList(extraLabels []string, dnsDomain string) []string {
	fqdnList := make([]string, 0, len(extraLabels))
	for _, label := range extraLabels {
		fqdn := fmt.Sprintf("%s.%s", label, dnsDomain)
		fqdnList = append(fqdnList, fqdn)
	}
	return fqdnList
}

// PostureEvaluationView is the JSON shape the dashboard renders on
// the peer detail page's Posture Status panel. Defined locally
// instead of leaning on the autogenerated api types because the
// evaluation timeline is dashboard-side UX, not OpenAPI surface —
// keeping it out of types.gen.go avoids cluttering the spec with a
// transient view-model.
type PostureEvaluationView struct {
	ID             uint64    `json:"id"`
	PostureCheckID string    `json:"posture_check_id"`
	CheckType      string    `json:"check_type"`
	Compliant      bool      `json:"compliant"`
	Reason         string    `json:"reason"`
	EvaluatedAt    time.Time `json:"evaluated_at"`
}

// GetPostureEvaluations answers GET /peers/{peerId}/posture-evaluations.
// Admin-only — the row carries which checks failed and why, which is
// effectively a peer's compliance trail; regular users don't get to
// see that across the account.
//
// Optional query param: ?limit=N (capped at 500 by the store).
func (h *Handler) GetPostureEvaluations(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	if h.postureEvalStore == nil {
		// Posture-eval store isn't wired (typically: management
		// running on a non-SQL store, or feature disabled). Return
		// an empty list rather than a 500 so the dashboard renders
		// gracefully.
		util.WriteJSONObject(r.Context(), w, []PostureEvaluationView{})
		return
	}

	accountID, userID := userAuth.AccountId, userAuth.UserId

	vars := mux.Vars(r)
	peerID := vars["peerId"]
	if peerID == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "invalid peer ID"), w)
		return
	}

	user, err := h.accountManager.GetUserByID(r.Context(), userID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	if !user.HasAdminPower() && !user.IsServiceUser {
		util.WriteError(r.Context(), status.Errorf(status.PermissionDenied, "admin power required"), w)
		return
	}

	limit := 100
	if q := r.URL.Query().Get("limit"); q != "" {
		if v, perr := strconv.Atoi(q); perr == nil && v > 0 {
			limit = v
		}
	}

	rows, err := h.postureEvalStore.ListForPeer(r.Context(), accountID, peerID, limit)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	out := make([]PostureEvaluationView, 0, len(rows))
	for _, row := range rows {
		out = append(out, PostureEvaluationView{
			ID:             row.ID,
			PostureCheckID: row.PostureCheckID,
			CheckType:      row.CheckType,
			Compliant:      row.Compliant,
			Reason:         row.Reason,
			EvaluatedAt:    row.EvaluatedAt,
		})
	}

	util.WriteJSONObject(r.Context(), w, out)
}
