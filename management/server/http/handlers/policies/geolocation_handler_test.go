package policies

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"path/filepath"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"

	nbcontext "github.com/openzro/openzro/management/server/context"
	"github.com/openzro/openzro/management/server/geolocation"
	"github.com/openzro/openzro/management/server/http/api"
	"github.com/openzro/openzro/management/server/mock_server"
	"github.com/openzro/openzro/management/server/permissions"
	"github.com/openzro/openzro/management/server/permissions/modules"
	"github.com/openzro/openzro/management/server/permissions/operations"
	"github.com/openzro/openzro/management/server/types"
	"github.com/openzro/openzro/util"
)

func initGeolocationTestData(t *testing.T) *geolocationsHandler {
	t.Helper()

	var (
		mmdbPath       = "../../../testdata/GeoLite2-City_20240305.mmdb"
		geonamesdbPath = "../../../testdata/geonames_20240305.db"
	)

	tempDir := t.TempDir()

	err := util.CopyFileContents(mmdbPath, path.Join(tempDir, filepath.Base(mmdbPath)))
	assert.NoError(t, err)

	err = util.CopyFileContents(geonamesdbPath, path.Join(tempDir, filepath.Base(geonamesdbPath)))
	assert.NoError(t, err)

	// autoUpdate=false + a staged mmdb + geonames in tempDir means
	// NewGeolocation never reaches for the network, so an empty
	// DownloadSource is fine here.
	geo, err := geolocation.NewGeolocation(context.Background(), tempDir, false, geolocation.DownloadSource{})
	assert.NoError(t, err)
	t.Cleanup(func() { _ = geo.Stop() })

	ctrl := gomock.NewController(t)
	permissionsManagerMock := permissions.NewMockManager(ctrl)
	permissionsManagerMock.
		EXPECT().
		ValidateUserPermissions(gomock.Any(), gomock.Any(), gomock.Any(), modules.Policies, operations.Read).
		Return(true, nil).
		AnyTimes()

	return &geolocationsHandler{
		accountManager: &mock_server.MockAccountManager{
			GetUserByIDFunc: func(ctx context.Context, id string) (*types.User, error) {
				return types.NewAdminUser(id), nil
			},
		},
		geolocationManager: geo,
		permissionsManager: permissionsManagerMock,
	}
}

func TestGetCitiesByCountry(t *testing.T) {
	tt := []struct {
		name           string
		expectedStatus int
		expectedBody   bool
		expectedCities []api.City
		requestType    string
		requestPath    string
	}{
		{
			name:           "Get cities with valid country iso code",
			expectedStatus: http.StatusOK,
			expectedBody:   true,
			expectedCities: []api.City{
				{
					CityName:  "Souni",
					GeonameId: 5819,
				},
				{
					CityName:  "Protaras",
					GeonameId: 18918,
				},
			},
			requestType: http.MethodGet,
			requestPath: "/api/locations/countries/CY/cities",
		},
		{
			name:           "Get cities with valid country iso code but zero cities",
			expectedStatus: http.StatusOK,
			expectedBody:   true,
			expectedCities: make([]api.City, 0),
			requestType:    http.MethodGet,
			requestPath:    "/api/locations/countries/DE/cities",
		},
		{
			name:           "Get cities with invalid country iso code",
			expectedStatus: http.StatusUnprocessableEntity,
			expectedBody:   false,
			requestType:    http.MethodGet,
			requestPath:    "/api/locations/countries/12ds/cities",
		},
	}

	geolocationHandler := initGeolocationTestData(t)

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(tc.requestType, tc.requestPath, nil)
			req = nbcontext.SetUserAuthInRequest(req, nbcontext.UserAuth{
				UserId:    "test_user",
				Domain:    "hotmail.com",
				AccountId: "test_id",
			})

			router := mux.NewRouter()
			router.HandleFunc("/api/locations/countries/{country}/cities", geolocationHandler.getCitiesByCountry).Methods("GET")
			router.ServeHTTP(recorder, req)

			res := recorder.Result()
			defer res.Body.Close()

			content, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatalf("I don't know what I expected; %v", err)
				return
			}

			if status := recorder.Code; status != tc.expectedStatus {
				t.Errorf("handler returned wrong status code: got %v want %v, content: %s",
					status, tc.expectedStatus, string(content))
				return
			}

			if !tc.expectedBody {
				return
			}

			cities := make([]api.City, 0)
			if err = json.Unmarshal(content, &cities); err != nil {
				t.Fatalf("unmarshal request cities response : %v", err)
				return
			}
			assert.ElementsMatch(t, tc.expectedCities, cities)
		})
	}
}

func TestGetAllCountries(t *testing.T) {
	tt := []struct {
		name              string
		expectedStatus    int
		expectedBody      bool
		expectedCountries []api.Country
		requestType       string
		requestPath       string
	}{
		{
			name:           "Get all countries",
			expectedStatus: http.StatusOK,
			expectedBody:   true,
			expectedCountries: []api.Country{
				{
					CountryCode: "IR",
					CountryName: "Iran",
				},
				{
					CountryCode: "CY",
					CountryName: "Cyprus",
				},
				{
					CountryCode: "RW",
					CountryName: "Rwanda",
				},
				{
					CountryCode: "SO",
					CountryName: "Somalia",
				},
				{
					CountryCode: "YE",
					CountryName: "Yemen",
				},
				{
					CountryCode: "LY",
					CountryName: "Libya",
				},
				{
					CountryCode: "IQ",
					CountryName: "Iraq",
				},
			},
			requestType: http.MethodGet,
			requestPath: "/api/locations/countries",
		},
	}

	geolocationHandler := initGeolocationTestData(t)

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(tc.requestType, tc.requestPath, nil)
			req = nbcontext.SetUserAuthInRequest(req, nbcontext.UserAuth{
				UserId:    "test_user",
				Domain:    "hotmail.com",
				AccountId: "test_id",
			})

			router := mux.NewRouter()
			router.HandleFunc("/api/locations/countries", geolocationHandler.getAllCountries).Methods("GET")
			router.ServeHTTP(recorder, req)

			res := recorder.Result()
			defer res.Body.Close()

			content, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatalf("I don't know what I expected; %v", err)
				return
			}

			if status := recorder.Code; status != tc.expectedStatus {
				t.Errorf("handler returned wrong status code: got %v want %v, content: %s",
					status, tc.expectedStatus, string(content))
				return
			}

			if !tc.expectedBody {
				return
			}

			countries := make([]api.Country, 0)
			if err = json.Unmarshal(content, &countries); err != nil {
				t.Fatalf("unmarshal request cities response : %v", err)
				return
			}
			assert.ElementsMatch(t, tc.expectedCountries, countries)
		})
	}
}

// Regression: when the geo DB hasn't been provisioned, the handlers
// must respond with 200 + an empty list rather than 412. The dashboard
// mounts CountryProvider at the layout root, so any non-2xx puts SWR
// into a retry loop on every page. Empty-list-as-success is the right
// signal: there are no countries we can resolve.
func TestGeolocationsHandler_GeoDBNotInitialized(t *testing.T) {
	ctrl := gomock.NewController(t)
	permissionsManagerMock := permissions.NewMockManager(ctrl)
	permissionsManagerMock.
		EXPECT().
		ValidateUserPermissions(gomock.Any(), gomock.Any(), gomock.Any(), modules.Policies, operations.Read).
		Return(true, nil).
		AnyTimes()

	handler := &geolocationsHandler{
		accountManager: &mock_server.MockAccountManager{
			GetUserByIDFunc: func(ctx context.Context, id string) (*types.User, error) {
				return types.NewAdminUser(id), nil
			},
		},
		geolocationManager: nil, // simulate "geo DB never provisioned"
		permissionsManager: permissionsManagerMock,
	}

	cases := []struct {
		name        string
		path        string
		register    func(*mux.Router)
		decodeEmpty func(*testing.T, []byte)
	}{
		{
			name: "countries returns 200 + empty list",
			path: "/api/locations/countries",
			register: func(r *mux.Router) {
				r.HandleFunc("/api/locations/countries", handler.getAllCountries).Methods("GET")
			},
			decodeEmpty: func(t *testing.T, body []byte) {
				out := make([]api.Country, 0)
				assert.NoError(t, json.Unmarshal(body, &out))
				assert.Empty(t, out)
			},
		},
		{
			name: "cities returns 200 + empty list",
			path: "/api/locations/countries/CY/cities",
			register: func(r *mux.Router) {
				r.HandleFunc("/api/locations/countries/{country}/cities", handler.getCitiesByCountry).Methods("GET")
			},
			decodeEmpty: func(t *testing.T, body []byte) {
				out := make([]api.City, 0)
				assert.NoError(t, json.Unmarshal(body, &out))
				assert.Empty(t, out)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req = nbcontext.SetUserAuthInRequest(req, nbcontext.UserAuth{
				UserId:    "test_user",
				Domain:    "hotmail.com",
				AccountId: "test_id",
			})

			router := mux.NewRouter()
			tc.register(router)
			router.ServeHTTP(recorder, req)

			res := recorder.Result()
			defer res.Body.Close()
			body, err := io.ReadAll(res.Body)
			assert.NoError(t, err)

			assert.Equal(t, http.StatusOK, recorder.Code,
				"expected 200, got %d (body: %s)", recorder.Code, string(body))
			tc.decodeEmpty(t, body)
		})
	}
}
