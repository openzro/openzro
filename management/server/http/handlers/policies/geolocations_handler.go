package policies

import (
	"net/http"
	"regexp"

	"github.com/gorilla/mux"

	"github.com/openzro/openzro/management/server/account"
	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/geolocation"
	"github.com/openzro/openzro/management/server/http/api"
	"github.com/openzro/openzro/management/server/http/util"
	"github.com/openzro/openzro/management/server/permissions"
	"github.com/openzro/openzro/management/server/permissions/modules"
	"github.com/openzro/openzro/management/server/permissions/operations"
	"github.com/openzro/openzro/management/server/status"
)

var (
	countryCodeRegex = regexp.MustCompile("^[a-zA-Z]{2}$")
)

// geolocationsHandler is a handler that returns locations.
type geolocationsHandler struct {
	accountManager     account.Manager
	geolocationManager geolocation.Geolocation
	permissionsManager permissions.Manager
}

func AddLocationsEndpoints(accountManager account.Manager, locationManager geolocation.Geolocation, permissionsManager permissions.Manager, router *mux.Router) {
	locationHandler := newGeolocationsHandlerHandler(accountManager, locationManager, permissionsManager)
	router.HandleFunc("/locations/countries", locationHandler.getAllCountries).Methods("GET", "OPTIONS")
	router.HandleFunc("/locations/countries/{country}/cities", locationHandler.getCitiesByCountry).Methods("GET", "OPTIONS")
}

// newGeolocationsHandlerHandler creates a new Geolocations handler
func newGeolocationsHandlerHandler(accountManager account.Manager, geolocationManager geolocation.Geolocation, permissionsManager permissions.Manager) *geolocationsHandler {
	return &geolocationsHandler{
		accountManager:     accountManager,
		geolocationManager: geolocationManager,
		permissionsManager: permissionsManager,
	}
}

// getAllCountries retrieves a list of all countries
func (l *geolocationsHandler) getAllCountries(w http.ResponseWriter, r *http.Request) {
	if err := l.authenticateUser(r); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	// CountryProvider in the dashboard is mounted at the dashboard layout
	// root, so this endpoint fires on every page. Returning 412 here put
	// SWR into an unbounded retry loop on every dev environment that
	// doesn't have a GeoLite2 DB on disk. An empty list is a truthful
	// response — there are no countries we can resolve — and the
	// dashboard already treats unknown country codes as "Unknown".
	if l.geolocationManager == nil {
		util.WriteJSONObject(r.Context(), w, []api.Country{})
		return
	}

	allCountries, err := l.geolocationManager.GetAllCountries()
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	countries := make([]api.Country, 0, len(allCountries))
	for _, country := range allCountries {
		countries = append(countries, toCountryResponse(country))
	}
	util.WriteJSONObject(r.Context(), w, countries)
}

// getCitiesByCountry retrieves a list of cities based on the given country code
func (l *geolocationsHandler) getCitiesByCountry(w http.ResponseWriter, r *http.Request) {
	if err := l.authenticateUser(r); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	vars := mux.Vars(r)
	countryCode := vars["country"]
	if !countryCodeRegex.MatchString(countryCode) {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "invalid country code"), w)
		return
	}

	// Same rationale as getAllCountries — empty list keeps clients quiet
	// when the geo DB hasn't been provisioned. Operators who *do* want
	// the feature get the same 200 with [] until they configure the DB,
	// at which point the path lights up automatically.
	if l.geolocationManager == nil {
		util.WriteJSONObject(r.Context(), w, []api.City{})
		return
	}

	allCities, err := l.geolocationManager.GetCitiesByCountry(countryCode)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	cities := make([]api.City, 0, len(allCities))
	for _, city := range allCities {
		cities = append(cities, toCityResponse(city))
	}
	util.WriteJSONObject(r.Context(), w, cities)
}

func (l *geolocationsHandler) authenticateUser(r *http.Request) error {
	ctx := r.Context()

	userAuth, err := nbcontext.GetUserAuthFromContext(ctx)
	if err != nil {
		return err
	}

	accountID, userID := userAuth.AccountId, userAuth.UserId

	allowed, err := l.permissionsManager.ValidateUserPermissions(ctx, accountID, userID, modules.Policies, operations.Read)
	if err != nil {
		return status.NewPermissionValidationError(err)
	}

	if !allowed {
		return status.NewPermissionDeniedError()
	}
	return nil
}

func toCountryResponse(country geolocation.Country) api.Country {
	return api.Country{
		CountryName: country.CountryName,
		CountryCode: country.CountryISOCode,
	}
}

func toCityResponse(city geolocation.City) api.City {
	return api.City{
		CityName:  city.CityName,
		GeonameId: city.GeoNameID,
	}
}
