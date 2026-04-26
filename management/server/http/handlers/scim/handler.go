package scim

import (
	"github.com/gorilla/mux"

	"github.com/openzro/openzro/management/server/account"
)

// Handler hosts the SCIM 2.0 endpoints. It stays tiny on purpose:
// every method is a thin adapter from SCIM wire shapes to the existing
// account.Manager methods.
type Handler struct {
	accountManager account.Manager
}

// AddEndpoints registers the SCIM 2.0 routes on the given subrouter.
// The subrouter is expected to be mounted at /scim/v2 and to have
// the project's auth middleware applied; this function does not
// install auth itself.
//
// Routes:
//
//	GET    /ServiceProviderConfig
//	GET    /Schemas
//	GET    /ResourceTypes
//	GET    /Users                   list (with userName filter + paging)
//	POST   /Users                   create
//	GET    /Users/{id}              read
//	PUT    /Users/{id}              full replace
//	PATCH  /Users/{id}              partial update
//	DELETE /Users/{id}              soft delete (active=false)
func AddEndpoints(accountManager account.Manager, router *mux.Router) {
	h := &Handler{accountManager: accountManager}
	router.HandleFunc("/ServiceProviderConfig", h.handleServiceProviderConfig).Methods("GET", "OPTIONS")
	router.HandleFunc("/Schemas", h.handleSchemas).Methods("GET", "OPTIONS")
	router.HandleFunc("/ResourceTypes", h.handleResourceTypes).Methods("GET", "OPTIONS")

	router.HandleFunc("/Users", h.handleListUsers).Methods("GET", "OPTIONS")
	router.HandleFunc("/Users", h.handleCreateUser).Methods("POST", "OPTIONS")
	router.HandleFunc("/Users/{id}", h.handleGetUser).Methods("GET", "OPTIONS")
	router.HandleFunc("/Users/{id}", h.handleReplaceUser).Methods("PUT", "OPTIONS")
	router.HandleFunc("/Users/{id}", h.handlePatchUser).Methods("PATCH", "OPTIONS")
	router.HandleFunc("/Users/{id}", h.handleDeleteUser).Methods("DELETE", "OPTIONS")

	router.HandleFunc("/Groups", h.handleListGroups).Methods("GET", "OPTIONS")
	router.HandleFunc("/Groups", h.handleCreateGroup).Methods("POST", "OPTIONS")
	router.HandleFunc("/Groups/{id}", h.handleGetGroup).Methods("GET", "OPTIONS")
	router.HandleFunc("/Groups/{id}", h.handleReplaceGroup).Methods("PUT", "OPTIONS")
	router.HandleFunc("/Groups/{id}", h.handlePatchGroup).Methods("PATCH", "OPTIONS")
	router.HandleFunc("/Groups/{id}", h.handleDeleteGroup).Methods("DELETE", "OPTIONS")
}
