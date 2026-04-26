package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/openzro/openzro/management/server/http/util"
)

// Client Management service HTTP REST API Client
type Client struct {
	managementURL string
	authHeader    string
	httpClient    HttpClient

	// Accounts Openzro account APIs
	// see more: https://docs.openzro.io/api/resources/accounts
	Accounts *AccountsAPI

	// Users Openzro users APIs
	// see more: https://docs.openzro.io/api/resources/users
	Users *UsersAPI

	// Tokens Openzro tokens APIs
	// see more: https://docs.openzro.io/api/resources/tokens
	Tokens *TokensAPI

	// Peers Openzro peers APIs
	// see more: https://docs.openzro.io/api/resources/peers
	Peers *PeersAPI

	// SetupKeys Openzro setup keys APIs
	// see more: https://docs.openzro.io/api/resources/setup-keys
	SetupKeys *SetupKeysAPI

	// Groups Openzro groups APIs
	// see more: https://docs.openzro.io/api/resources/groups
	Groups *GroupsAPI

	// Policies Openzro policies APIs
	// see more: https://docs.openzro.io/api/resources/policies
	Policies *PoliciesAPI

	// PostureChecks Openzro posture checks APIs
	// see more: https://docs.openzro.io/api/resources/posture-checks
	PostureChecks *PostureChecksAPI

	// Networks Openzro networks APIs
	// see more: https://docs.openzro.io/api/resources/networks
	Networks *NetworksAPI

	// Routes Openzro routes APIs
	// see more: https://docs.openzro.io/api/resources/routes
	Routes *RoutesAPI

	// DNS Openzro DNS APIs
	// see more: https://docs.openzro.io/api/resources/routes
	DNS *DNSAPI

	// GeoLocation Openzro Geo Location APIs
	// see more: https://docs.openzro.io/api/resources/geo-locations
	GeoLocation *GeoLocationAPI

	// Events Openzro Events APIs
	// see more: https://docs.openzro.io/api/resources/events
	Events *EventsAPI
}

// New initialize new Client instance using PAT token
func New(managementURL, token string) *Client {
	return NewWithOptions(
		WithManagementURL(managementURL),
		WithPAT(token),
	)
}

// NewWithBearerToken initialize new Client instance using Bearer token type
func NewWithBearerToken(managementURL, token string) *Client {
	return NewWithOptions(
		WithManagementURL(managementURL),
		WithBearerToken(token),
	)
}

// NewWithOptions initialize new Client instance with options
func NewWithOptions(opts ...option) *Client {
	client := &Client{
		httpClient: http.DefaultClient,
	}

	for _, option := range opts {
		option(client)
	}

	client.initialize()
	return client
}

func (c *Client) initialize() {
	c.Accounts = &AccountsAPI{c}
	c.Users = &UsersAPI{c}
	c.Tokens = &TokensAPI{c}
	c.Peers = &PeersAPI{c}
	c.SetupKeys = &SetupKeysAPI{c}
	c.Groups = &GroupsAPI{c}
	c.Policies = &PoliciesAPI{c}
	c.PostureChecks = &PostureChecksAPI{c}
	c.Networks = &NetworksAPI{c}
	c.Routes = &RoutesAPI{c}
	c.DNS = &DNSAPI{c}
	c.GeoLocation = &GeoLocationAPI{c}
	c.Events = &EventsAPI{c}
}

// NewRequest creates and executes new management API request
func (c *Client) NewRequest(ctx context.Context, method, path string, body io.Reader, query map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.managementURL+path, body)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Authorization", c.authHeader)
	req.Header.Add("Accept", "application/json")
	if body != nil {
		req.Header.Add("Content-Type", "application/json")
	}

	if len(query) != 0 {
		q := req.URL.Query()
		for k, v := range query {
			q.Add(k, v)
		}
		req.URL.RawQuery = q.Encode()
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode > 299 {
		parsedErr, pErr := parseResponse[util.ErrorResponse](resp)
		if pErr != nil {

			return nil, pErr
		}
		return nil, errors.New(parsedErr.Message)
	}

	return resp, nil
}

func parseResponse[T any](resp *http.Response) (T, error) {
	var ret T
	if resp.Body == nil {
		return ret, fmt.Errorf("Body missing, HTTP Error code %d", resp.StatusCode)
	}
	bs, err := io.ReadAll(resp.Body)
	if err != nil {
		return ret, err
	}
	err = json.Unmarshal(bs, &ret)
	if err != nil {
		return ret, fmt.Errorf("Error code %d, error unmarshalling body: %w", resp.StatusCode, err)
	}

	return ret, nil
}
