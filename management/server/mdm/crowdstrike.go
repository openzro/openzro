package mdm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// CrowdStrike is the CrowdStrike Falcon (EDR / sensor) provider.
//
// Authentication: OAuth2 client_credentials. The Falcon API client is
// minted in the console under Support → API Clients and Keys; the
// scope set we need is `Hosts: Read`. Without it the token request
// succeeds but the device endpoints return 403 — fail closed and
// surface an actionable error.
//
// Lookup is two-step against Falcon's Hosts API:
//
//  1. GET /devices/queries/devices/v1?filter=hostname:'<host>' → list
//     of device AIDs (Falcon's internal device IDs).
//  2. GET /devices/entities/devices/v2?ids=<aid> → the device record.
//
// Compliance rules (see statusFromCSDevice):
//   - status == "normal" → compliant
//   - status == "containment_pending" / "contained" / "lift_containment_pending"
//     → non-compliant (active threat response, host is being isolated)
//   - reduced_functionality_mode != "no" → non-compliant (sensor is
//     impaired and not actively defending the host)
//
// Region awareness: Falcon tenants live in regional clouds (us-1,
// us-2, eu-1, us-gov-1, us-gov-2). The same OAuth client only works
// in its home region; pointing at the wrong base URL returns 401.
type CrowdStrike struct {
	cfg     CrowdStrikeConfig
	baseURL string
	client  *http.Client
}

// NewCrowdStrike constructs the driver.
func NewCrowdStrike(cfg CrowdStrikeConfig) (*CrowdStrike, error) {
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, errors.New("crowdstrike: client_id and client_secret are required")
	}
	base, err := cloudBaseURL(cfg.Cloud)
	if err != nil {
		return nil, err
	}

	ccCfg := &clientcredentials.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		TokenURL:     base + "/oauth2/token",
		AuthStyle:    oauth2.AuthStyleInParams,
	}
	httpClient := ccCfg.Client(context.Background())
	httpClient.Timeout = 30 * time.Second

	return &CrowdStrike{cfg: cfg, baseURL: base, client: httpClient}, nil
}

func (c *CrowdStrike) Type() ProviderType { return TypeCrowdStrike }
func (c *CrowdStrike) Close() error       { return nil }

// cloudBaseURL maps the configured cloud to its API base URL.
// Empty value defaults to us-1, the original public cloud.
func cloudBaseURL(cloud string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(cloud)) {
	case "", "us-1":
		return "https://api.crowdstrike.com", nil
	case "us-2":
		return "https://api.us-2.crowdstrike.com", nil
	case "eu-1":
		return "https://api.eu-1.crowdstrike.com", nil
	case "us-gov-1":
		return "https://api.laggar.gcw.crowdstrike.com", nil
	case "us-gov-2":
		return "https://api.us-gov-2.crowdstrike.mil", nil
	}
	return "", fmt.Errorf("crowdstrike: unknown cloud %q (valid: us-1, us-2, eu-1, us-gov-1, us-gov-2)", cloud)
}

// csQueryResp is the relevant subset of the device-query response.
type csQueryResp struct {
	Resources []string `json:"resources"`
}

// csEntitiesResp is the relevant subset of the device-entities response.
type csEntitiesResp struct {
	Resources []csDevice `json:"resources"`
}

type csDevice struct {
	DeviceID                 string `json:"device_id"`
	Hostname                 string `json:"hostname"`
	Status                   string `json:"status"`
	ReducedFunctionalityMode string `json:"reduced_functionality_mode"`
	OSVersion                string `json:"os_version"`
	AgentVersion             string `json:"agent_version"`
	LastSeen                 string `json:"last_seen"`
}

// GetDeviceStatus queries Falcon for the device by hostname and
// translates the response into DeviceStatus.
func (c *CrowdStrike) GetDeviceStatus(ctx context.Context, lookup DeviceLookup) (DeviceStatus, error) {
	// Falcon hosts are keyed by hostname; per-user attribution isn't
	// part of the host lookup, so we ignore lookup.UserEmail.
	return c.lookupAt(ctx, c.baseURL, lookup.Hostname)
}

// lookupAt is GetDeviceStatus parameterised on the Falcon base URL —
// tests inject an httptest server here.
func (c *CrowdStrike) lookupAt(ctx context.Context, base, hostname string) (DeviceStatus, error) {
	if hostname == "" {
		return DeviceStatus{}, errors.New("crowdstrike: hostname is empty")
	}
	base = strings.TrimRight(base, "/")

	// Step 1: hostname → AID list.
	filter := fmt.Sprintf(`hostname:'%s'`, escapeFalconFilter(hostname))
	queryURL := fmt.Sprintf(
		"%s/devices/queries/devices/v1?filter=%s",
		base, url.QueryEscape(filter),
	)
	aids, err := c.fetchAIDs(ctx, queryURL)
	if err != nil {
		return DeviceStatus{}, err
	}
	if len(aids) == 0 {
		return DeviceStatus{
			Found:     false,
			Compliant: false,
			Reason:    fmt.Sprintf("hostname '%s' has no Falcon sensor registered", hostname),
		}, nil
	}

	// Step 2: AID → full device record. Use the first match — if a
	// hostname collides across hosts (renamed boxes etc.), Falcon
	// returns them in last-seen-desc order by default.
	entURL := fmt.Sprintf("%s/devices/entities/devices/v2?ids=%s",
		base, url.QueryEscape(aids[0]))
	dev, err := c.fetchDevice(ctx, entURL)
	if err != nil {
		return DeviceStatus{}, err
	}
	return statusFromCSDevice(dev), nil
}

func (c *CrowdStrike) fetchAIDs(ctx context.Context, target string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("crowdstrike: query api: %w", err)
	}
	defer drainAndClose(resp.Body)

	if err := mapFalconStatus(resp.StatusCode); err != nil {
		return nil, err
	}
	var body csQueryResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("crowdstrike: decode query response: %w", err)
	}
	return body.Resources, nil
}

func (c *CrowdStrike) fetchDevice(ctx context.Context, target string) (csDevice, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return csDevice{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return csDevice{}, fmt.Errorf("crowdstrike: entities api: %w", err)
	}
	defer drainAndClose(resp.Body)

	if err := mapFalconStatus(resp.StatusCode); err != nil {
		return csDevice{}, err
	}
	var body csEntitiesResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return csDevice{}, fmt.Errorf("crowdstrike: decode entities response: %w", err)
	}
	if len(body.Resources) == 0 {
		return csDevice{}, errors.New("crowdstrike: AID resolved by query but entities returned no record")
	}
	return body.Resources[0], nil
}

// statusFromCSDevice applies the compliance rules. A device in active
// containment is treated as non-compliant — Falcon containment is the
// security team's "isolate this host now" action; the box should not
// be admitted into the network until containment is lifted.
func statusFromCSDevice(d csDevice) DeviceStatus {
	switch strings.ToLower(d.Status) {
	case "contained":
		return DeviceStatus{
			Found:     true,
			Compliant: false,
			Reason:    "Falcon: host is contained (network isolation active)",
		}
	case "containment_pending":
		return DeviceStatus{
			Found:     true,
			Compliant: false,
			Reason:    "Falcon: containment pending (host is being isolated)",
		}
	case "lift_containment_pending":
		return DeviceStatus{
			Found:     true,
			Compliant: false,
			Reason:    "Falcon: lifting containment (host not yet released)",
		}
	}
	if rfm := strings.ToLower(d.ReducedFunctionalityMode); rfm != "" && rfm != "no" {
		return DeviceStatus{
			Found:     true,
			Compliant: false,
			Reason:    fmt.Sprintf("Falcon sensor in reduced_functionality_mode=%q", d.ReducedFunctionalityMode),
		}
	}
	if strings.ToLower(d.Status) != "normal" {
		return DeviceStatus{
			Found:     true,
			Compliant: false,
			Reason:    fmt.Sprintf("Falcon status=%q (expected 'normal')", d.Status),
		}
	}
	return DeviceStatus{
		Found:     true,
		Compliant: true,
		Reason:    "Falcon: sensor normal",
	}
}

// mapFalconStatus turns non-OK HTTP into actionable errors. 401 most
// often means the OAuth client is from a different cloud than the one
// configured; 403 means the API client lacks the Hosts:Read scope.
func mapFalconStatus(code int) error {
	switch code {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized:
		return errors.New("crowdstrike: 401 from Falcon — check that the configured cloud matches the API client's home region")
	case http.StatusForbidden:
		return errors.New("crowdstrike: 403 from Falcon — API client needs the Hosts:Read scope")
	}
	return fmt.Errorf("crowdstrike: api returned %d", code)
}

// escapeFalconFilter handles Falcon FQL quoting: a literal single
// quote inside a quoted string is escaped by a backslash.
func escapeFalconFilter(s string) string {
	return strings.ReplaceAll(s, `'`, `\'`)
}

func drainAndClose(rc io.ReadCloser) {
	_, _ = io.Copy(io.Discard, rc)
	_ = rc.Close()
}
