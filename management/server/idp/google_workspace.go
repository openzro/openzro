package idp

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	"cloud.google.com/go/auth"
	authcreds "cloud.google.com/go/auth/credentials"
	log "github.com/sirupsen/logrus"
	admin "google.golang.org/api/admin/directory/v1"
	"google.golang.org/api/option"

	"github.com/openzro/openzro/management/server/telemetry"
)

// GoogleWorkspaceManager Google Workspace manager client instance.
type GoogleWorkspaceManager struct {
	usersService *admin.UsersService
	CustomerID   string
	httpClient   ManagerHTTPClient
	credentials  ManagerCredentials
	helper       ManagerHelper
	appMetrics   telemetry.AppMetrics
}

// GoogleWorkspaceClientConfig Google Workspace manager client configurations.
type GoogleWorkspaceClientConfig struct {
	ServiceAccountKey string
	CustomerID        string
}

// GoogleWorkspaceCredentials Google Workspace authentication information.
type GoogleWorkspaceCredentials struct {
	clientConfig GoogleWorkspaceClientConfig
	helper       ManagerHelper
	httpClient   ManagerHTTPClient
	appMetrics   telemetry.AppMetrics
}

func (gc *GoogleWorkspaceCredentials) Authenticate(_ context.Context) (JWTToken, error) {
	return JWTToken{}, nil
}

// NewGoogleWorkspaceManager creates a new instance of the GoogleWorkspaceManager.
func NewGoogleWorkspaceManager(ctx context.Context, config GoogleWorkspaceClientConfig, appMetrics telemetry.AppMetrics) (*GoogleWorkspaceManager, error) {
	httpTransport := http.DefaultTransport.(*http.Transport).Clone()
	httpTransport.MaxIdleConns = 5

	httpClient := &http.Client{
		Timeout:   10 * time.Second,
		Transport: httpTransport,
	}
	helper := JsonParser{}

	if config.CustomerID == "" {
		return nil, fmt.Errorf("google IdP configuration is incomplete, CustomerID is missing")
	}

	credentials := &GoogleWorkspaceCredentials{
		clientConfig: config,
		httpClient:   httpClient,
		helper:       helper,
		appMetrics:   appMetrics,
	}

	// Create a new Admin SDK Directory service client
	adminCredentials, err := getGoogleCredentials(ctx, config.ServiceAccountKey)
	if err != nil {
		return nil, err
	}

	service, err := admin.NewService(context.Background(),
		option.WithScopes(admin.AdminDirectoryUserReadonlyScope),
		option.WithAuthCredentials(adminCredentials),
	)
	if err != nil {
		return nil, err
	}

	return &GoogleWorkspaceManager{
		usersService: service.Users,
		CustomerID:   config.CustomerID,
		httpClient:   httpClient,
		credentials:  credentials,
		helper:       helper,
		appMetrics:   appMetrics,
	}, nil
}

// UpdateUserAppMetadata updates user app metadata based on userID and metadata map.
func (gm *GoogleWorkspaceManager) UpdateUserAppMetadata(_ context.Context, _ string, _ AppMetadata) error {
	return nil
}

// GetUserDataByID requests user data from Google Workspace via ID.
func (gm *GoogleWorkspaceManager) GetUserDataByID(_ context.Context, userID string, appMetadata AppMetadata) (*UserData, error) {
	user, err := gm.usersService.Get(userID).Do()
	if err != nil {
		return nil, err
	}

	if gm.appMetrics != nil {
		gm.appMetrics.IDPMetrics().CountGetUserDataByID()
	}

	userData := parseGoogleWorkspaceUser(user)
	userData.AppMetadata = appMetadata

	return userData, nil
}

// GetAccount returns all the users for a given profile.
func (gm *GoogleWorkspaceManager) GetAccount(_ context.Context, accountID string) ([]*UserData, error) {
	users, err := gm.getAllUsers()
	if err != nil {
		return nil, err
	}

	if gm.appMetrics != nil {
		gm.appMetrics.IDPMetrics().CountGetAccount()
	}

	for index, user := range users {
		user.AppMetadata.WTAccountID = accountID
		users[index] = user
	}

	return users, nil
}

// GetAllAccounts gets all registered accounts with corresponding user data.
// It returns a list of users indexed by accountID.
func (gm *GoogleWorkspaceManager) GetAllAccounts(_ context.Context) (map[string][]*UserData, error) {
	users, err := gm.getAllUsers()
	if err != nil {
		return nil, err
	}

	indexedUsers := make(map[string][]*UserData)
	indexedUsers[UnsetAccountID] = append(indexedUsers[UnsetAccountID], users...)

	if gm.appMetrics != nil {
		gm.appMetrics.IDPMetrics().CountGetAllAccounts()
	}

	return indexedUsers, nil
}

// getAllUsers returns all users in a Google Workspace account filtered by customer ID.
func (gm *GoogleWorkspaceManager) getAllUsers() ([]*UserData, error) {
	users := make([]*UserData, 0)
	pageToken := ""
	for {
		call := gm.usersService.List().Customer(gm.CustomerID).MaxResults(500)
		if pageToken != "" {
			call.PageToken(pageToken)
		}

		resp, err := call.Do()
		if err != nil {
			return nil, err
		}

		for _, user := range resp.Users {
			users = append(users, parseGoogleWorkspaceUser(user))
		}

		pageToken = resp.NextPageToken
		if pageToken == "" {
			break
		}
	}

	return users, nil
}

// CreateUser creates a new user in Google Workspace and sends an invitation.
func (gm *GoogleWorkspaceManager) CreateUser(_ context.Context, _, _, _, _ string) (*UserData, error) {
	return nil, fmt.Errorf("method CreateUser not implemented")
}

// GetUserByEmail searches users with a given email.
// If no users have been found, this function returns an empty list.
func (gm *GoogleWorkspaceManager) GetUserByEmail(_ context.Context, email string) ([]*UserData, error) {
	user, err := gm.usersService.Get(email).Do()
	if err != nil {
		return nil, err
	}

	if gm.appMetrics != nil {
		gm.appMetrics.IDPMetrics().CountGetUserByEmail()
	}

	users := make([]*UserData, 0)
	users = append(users, parseGoogleWorkspaceUser(user))

	return users, nil
}

// InviteUserByID resend invitations to users who haven't activated,
// their accounts prior to the expiration period.
func (gm *GoogleWorkspaceManager) InviteUserByID(_ context.Context, _ string) error {
	return fmt.Errorf("method InviteUserByID not implemented")
}

// DeleteUser from GoogleWorkspace.
func (gm *GoogleWorkspaceManager) DeleteUser(_ context.Context, userID string) error {
	if err := gm.usersService.Delete(userID).Do(); err != nil {
		return err
	}

	if gm.appMetrics != nil {
		gm.appMetrics.IDPMetrics().CountDeleteUser()
	}

	return nil
}

// getGoogleCredentials retrieves Google credentials based on the
// provided serviceAccountKey. It decodes the base64-encoded
// serviceAccountKey and attempts to obtain credentials using it. If
// that fails, it falls back to Application Default Credentials (ADC).
//
// Migrated off the SA1019-deprecated `golang.org/x/oauth2/google`
// surface (`CredentialsFromJSON` + `FindDefaultCredentials`) onto
// `cloud.google.com/go/auth/credentials.DetectDefault` — same fallback
// shape, current upstream API. The detector returns *auth.Credentials
// which the caller passes to `option.WithAuthCredentials` (the
// `option.WithCredentials` legacy surface is also SA1019-deprecated).
// Closes #82 along with the gcs.go sink call sites.
func getGoogleCredentials(ctx context.Context, serviceAccountKey string) (*auth.Credentials, error) {
	scopes := []string{admin.AdminDirectoryUserReadonlyScope}

	// Empty configuration: ADC only. Same chain the old code
	// reached via FindDefaultCredentials — env vars, gcloud user-
	// creds, GCE/GKE Workload Identity, Cloud Run/Functions
	// injected creds.
	if serviceAccountKey == "" {
		log.WithContext(ctx).Debug("no service account key configured, using application default credentials")
		return authcreds.DetectDefault(&authcreds.DetectOptions{Scopes: scopes})
	}

	log.WithContext(ctx).Debug("retrieving google credentials from the base64 encoded service account key")
	decodeKey, err := base64.StdEncoding.DecodeString(serviceAccountKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decode service account key: %w", err)
	}

	// Explicit key path is fail-CLOSED. NewCredentialsFromJSON PINS
	// the credential type to ServiceAccount, so a workforce-pool or
	// impersonated-SA JSON shape that an operator pasted by mistake
	// (or got from an untrusted source) is rejected at parse time —
	// that is the security upgrade the SA1019 deprecation flagged.
	//
	// Pre-migration the function fell back to ADC whenever the key
	// failed to parse, which silently substituted environment creds
	// for the operator's explicit (wrong-shaped) input. That hides
	// the misconfiguration and grants reach the operator did not
	// intend. We no longer fall back here: a non-empty key that
	// fails the type pin surfaces as a hard error.
	creds, err := authcreds.NewCredentialsFromJSON(
		authcreds.ServiceAccount,
		decodeKey,
		&authcreds.DetectOptions{Scopes: scopes},
	)
	if err != nil {
		return nil, fmt.Errorf("service account key: %w", err)
	}
	return creds, nil
}

// parseGoogleWorkspaceUser parse google user to UserData.
func parseGoogleWorkspaceUser(user *admin.User) *UserData {
	return &UserData{
		ID:    user.Id,
		Email: user.PrimaryEmail,
		Name:  user.Name.FullName,
	}
}
