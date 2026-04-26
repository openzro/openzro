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
// The subrouter is expected to be mounted at /scim/v2 and to have the
// project's auth middleware applied; this function does not install
// auth itself.
//
// Routes:
//
//	GET /ServiceProviderConfig  — capabilities advertisement
//	GET /Schemas                — schema catalog (URN listing only)
//	GET /ResourceTypes          — resource type catalog
//	GET /Users                  — list users in caller's account
//	GET /Users/{id}             — single-user lookup
func AddEndpoints(accountManager account.Manager, router *mux.Router) {
	h := &Handler{accountManager: accountManager}
	router.HandleFunc("/ServiceProviderConfig", h.handleServiceProviderConfig).Methods("GET", "OPTIONS")
	router.HandleFunc("/Schemas", h.handleSchemas).Methods("GET", "OPTIONS")
	router.HandleFunc("/ResourceTypes", h.handleResourceTypes).Methods("GET", "OPTIONS")
	router.HandleFunc("/Users", h.handleListUsers).Methods("GET", "OPTIONS")
	router.HandleFunc("/Users/{id}", h.handleGetUser).Methods("GET", "OPTIONS")
}
