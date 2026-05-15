package networks

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"

	flowstore "github.com/openzro/openzro/flow/store"
	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/groups"
	"github.com/openzro/openzro/management/server/http/api"
	"github.com/openzro/openzro/management/server/http/util"
	"github.com/openzro/openzro/management/server/networks/resources"
	"github.com/openzro/openzro/management/server/networks/resources/types"
)

// resolvedAddressesWindow is the look-back window for the
// `resolved_addresses` field on Domain-type NetworkResources. Fixed
// at 24h v1 — the dashboard's stated semantic ("currently resolves
// to") fits a recency window in roughly that range, and a fixed
// constant keeps the API surface small. Make this configurable if a
// future use case needs to peek further back (forensics) or tighter
// (only show what's hot right now).
const resolvedAddressesWindow = 24 * time.Hour

type resourceHandler struct {
	resourceManager resources.Manager
	groupsManager   groups.Manager
	// flowStore is the bridge to the agent-reported DNS resolutions
	// — see resolveDomainAddresses below. nil when the operator runs
	// without a flow store (engine=none); the handler degrades by
	// leaving the resolved_addresses field omitted from responses.
	flowStore flowstore.Store
}

func addResourceEndpoints(resourcesManager resources.Manager, groupsManager groups.Manager, flowStore flowstore.Store, router *mux.Router) {
	resourceHandler := newResourceHandler(resourcesManager, groupsManager, flowStore)
	router.HandleFunc("/networks/resources", resourceHandler.getAllResourcesInAccount).Methods("GET", "OPTIONS")
	router.HandleFunc("/networks/{networkId}/resources", resourceHandler.getAllResourcesInNetwork).Methods("GET", "OPTIONS")
	router.HandleFunc("/networks/{networkId}/resources", resourceHandler.createResource).Methods("POST", "OPTIONS")
	router.HandleFunc("/networks/{networkId}/resources/{resourceId}", resourceHandler.getResource).Methods("GET", "OPTIONS")
	router.HandleFunc("/networks/{networkId}/resources/{resourceId}", resourceHandler.updateResource).Methods("PUT", "OPTIONS")
	router.HandleFunc("/networks/{networkId}/resources/{resourceId}", resourceHandler.deleteResource).Methods("DELETE", "OPTIONS")
}

func newResourceHandler(resourceManager resources.Manager, groupsManager groups.Manager, flowStore flowstore.Store) *resourceHandler {
	return &resourceHandler{
		resourceManager: resourceManager,
		groupsManager:   groupsManager,
		flowStore:       flowStore,
	}
}

// resolveDomainAddresses returns the resource-ID → resolved-IPs map
// for the Domain-type resources in `list`. One query covers the
// whole listing path so we don't N+1 against the flow store on every
// resource. Errors are logged but never bubble up — the
// resolved_addresses field is decorative; the rest of the response
// is fine without it, and a transient flow-store hiccup must not
// 5xx the entire resources listing.
//
// Returns a nil map when there's nothing to query (no Domain
// resources, or no flow store configured); callers feed
// `m[id]` directly into ToAPIResponse, which tolerates nil entries.
func (h *resourceHandler) resolveDomainAddresses(ctx context.Context, accountID string, list []*types.NetworkResource) map[string][]string {
	if h.flowStore == nil || len(list) == 0 {
		return nil
	}
	domainIDs := make([]string, 0, len(list))
	for _, r := range list {
		if r != nil && r.Type == types.Domain {
			domainIDs = append(domainIDs, r.ID)
		}
	}
	if len(domainIDs) == 0 {
		return nil
	}
	since := time.Now().UTC().Add(-resolvedAddressesWindow)
	m, err := h.flowStore.ResolvedAddressesForResources(ctx, accountID, domainIDs, since)
	if err != nil {
		log.WithContext(ctx).Warnf("networks/resources: resolve domain addresses failed for account %s: %v", accountID, err)
		return nil
	}
	return m
}

func (h *resourceHandler) getAllResourcesInNetwork(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	accountID, userID := userAuth.AccountId, userAuth.UserId
	networkID := mux.Vars(r)["networkId"]
	resources, err := h.resourceManager.GetAllResourcesInNetwork(r.Context(), accountID, userID, networkID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	grps, err := h.groupsManager.GetAllGroups(r.Context(), accountID, userID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	grpsInfoMap := groups.ToGroupsInfoMap(grps, len(resources))
	resolved := h.resolveDomainAddresses(r.Context(), accountID, resources)

	var resourcesResponse []*api.NetworkResource
	for _, resource := range resources {
		resourcesResponse = append(resourcesResponse, resource.ToAPIResponse(grpsInfoMap[resource.ID], resolved[resource.ID]))
	}

	util.WriteJSONObject(r.Context(), w, resourcesResponse)
}
func (h *resourceHandler) getAllResourcesInAccount(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	accountID, userID := userAuth.AccountId, userAuth.UserId

	resources, err := h.resourceManager.GetAllResourcesInAccount(r.Context(), accountID, userID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	grps, err := h.groupsManager.GetAllGroups(r.Context(), accountID, userID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	grpsInfoMap := groups.ToGroupsInfoMap(grps, 0)
	resolved := h.resolveDomainAddresses(r.Context(), accountID, resources)

	resourcesResponse := make([]*api.NetworkResource, 0, len(resources))
	for _, resource := range resources {
		resourcesResponse = append(resourcesResponse, resource.ToAPIResponse(grpsInfoMap[resource.ID], resolved[resource.ID]))
	}

	util.WriteJSONObject(r.Context(), w, resourcesResponse)
}

func (h *resourceHandler) createResource(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	accountID, userID := userAuth.AccountId, userAuth.UserId

	var req api.NetworkResourceRequest
	err = json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		util.WriteErrorResponse("couldn't parse JSON request", http.StatusBadRequest, w)
		return
	}

	resource := &types.NetworkResource{}
	resource.FromAPIRequest(&req)

	resource.NetworkID = mux.Vars(r)["networkId"]
	resource.AccountID = accountID
	resource, err = h.resourceManager.CreateResource(r.Context(), userID, resource)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	grps, err := h.groupsManager.GetAllGroups(r.Context(), accountID, userID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	grpsInfoMap := groups.ToGroupsInfoMap(grps, 0)

	// Brand-new resource has no flow history yet — resolved_addresses
	// is naturally empty, no point querying the flow store.
	util.WriteJSONObject(r.Context(), w, resource.ToAPIResponse(grpsInfoMap[resource.ID], nil))
}

func (h *resourceHandler) getResource(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	accountID, userID := userAuth.AccountId, userAuth.UserId
	networkID := mux.Vars(r)["networkId"]
	resourceID := mux.Vars(r)["resourceId"]
	resource, err := h.resourceManager.GetResource(r.Context(), accountID, userID, networkID, resourceID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	grps, err := h.groupsManager.GetAllGroups(r.Context(), accountID, userID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	grpsInfoMap := groups.ToGroupsInfoMap(grps, 0)
	resolved := h.resolveDomainAddresses(r.Context(), accountID, []*types.NetworkResource{resource})

	util.WriteJSONObject(r.Context(), w, resource.ToAPIResponse(grpsInfoMap[resource.ID], resolved[resource.ID]))
}

func (h *resourceHandler) updateResource(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	accountID, userID := userAuth.AccountId, userAuth.UserId
	var req api.NetworkResourceRequest
	err = json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		util.WriteErrorResponse("couldn't parse JSON request", http.StatusBadRequest, w)
		return
	}

	resource := &types.NetworkResource{}
	resource.FromAPIRequest(&req)

	resource.ID = mux.Vars(r)["resourceId"]
	resource.NetworkID = mux.Vars(r)["networkId"]
	resource.AccountID = accountID
	resource, err = h.resourceManager.UpdateResource(r.Context(), userID, resource)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	grps, err := h.groupsManager.GetAllGroups(r.Context(), accountID, userID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	grpsInfoMap := groups.ToGroupsInfoMap(grps, 0)
	resolved := h.resolveDomainAddresses(r.Context(), accountID, []*types.NetworkResource{resource})

	util.WriteJSONObject(r.Context(), w, resource.ToAPIResponse(grpsInfoMap[resource.ID], resolved[resource.ID]))
}

func (h *resourceHandler) deleteResource(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	accountID, userID := userAuth.AccountId, userAuth.UserId

	networkID := mux.Vars(r)["networkId"]
	resourceID := mux.Vars(r)["resourceId"]
	err = h.resourceManager.DeleteResource(r.Context(), accountID, userID, networkID, resourceID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	util.WriteJSONObject(r.Context(), w, util.EmptyObject{})
}
