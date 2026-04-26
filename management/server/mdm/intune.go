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

// Intune is the Microsoft Intune (Endpoint Manager) provider.
//
// Authentication: OAuth client_credentials against the operator's
// Microsoft Entra (Azure AD) tenant. The app registration must have
// the Application permission `DeviceManagementManagedDevices.Read.All`
// granted with admin consent. Without it, Graph returns 403 and the
// posture check fails closed.
//
// Lookup: GET /v1.0/deviceManagement/managedDevices with a filter
// on deviceName eq '<host>'. We map complianceState=compliant
// (and the inGracePeriod variant) to DeviceStatus.Compliant=true;
// every other state including errors and "noncompliant" maps to
// false with a Reason carrying the raw state for the operator to
// debug.
//
// Token management: golang.org/x/oauth2/clientcredentials handles
// token acquisition and refresh transparently. The wrapped
// http.Client adds the Bearer header; we don't manage tokens by
// hand.
type Intune struct {
	cfg    IntuneConfig
	client *http.Client
}

// graphBase is overridden in tests via newIntuneTo.
const graphBase = "https://graph.microsoft.com"

// NewIntune constructs the Intune driver. Returns an error if
// required fields are missing or the OAuth wiring fails.
func NewIntune(cfg IntuneConfig) (*Intune, error) {
	if cfg.TenantID == "" || cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, errors.New("intune: tenant_id, client_id, and client_secret are required")
	}
	authority := cfg.Authority
	if authority == "" {
		authority = "https://login.microsoftonline.com"
	}
	tokenURL := fmt.Sprintf("%s/%s/oauth2/v2.0/token",
		strings.TrimRight(authority, "/"), cfg.TenantID)

	ccCfg := &clientcredentials.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		TokenURL:     tokenURL,
		Scopes:       []string{"https://graph.microsoft.com/.default"},
		AuthStyle:    oauth2.AuthStyleInParams,
	}
	httpClient := ccCfg.Client(context.Background())
	httpClient.Timeout = 30 * time.Second

	return &Intune{cfg: cfg, client: httpClient}, nil
}

func (i *Intune) Type() ProviderType { return TypeIntune }
func (i *Intune) Close() error       { return nil }

// graphResp is the relevant subset of the /managedDevices response.
type graphResp struct {
	Value []graphDevice `json:"value"`
}

type graphDevice struct {
	ID              string `json:"id"`
	DeviceName      string `json:"deviceName"`
	ComplianceState string `json:"complianceState"`
	ManagementState string `json:"managementState"`
	OperatingSystem string `json:"operatingSystem"`
	OSVersion       string `json:"osVersion"`
}

// GetDeviceStatus queries Graph for the device by deviceName and
// translates the response into DeviceStatus.
func (i *Intune) GetDeviceStatus(ctx context.Context, deviceID string) (DeviceStatus, error) {
	return i.lookupAt(ctx, graphBase, deviceID)
}

// lookupAt is GetDeviceStatus parameterised on the Graph base URL —
// tests inject an httptest server here.
func (i *Intune) lookupAt(ctx context.Context, base, deviceName string) (DeviceStatus, error) {
	if deviceName == "" {
		return DeviceStatus{}, errors.New("intune: deviceName is empty")
	}

	// Filter by deviceName equality. Intune deviceName is what the
	// MDM agent reported as the host's name — must match the
	// openZro peer's hostname for this to resolve.
	filter := fmt.Sprintf(`deviceName eq '%s'`, escapeFilterValue(deviceName))
	target := fmt.Sprintf(
		"%s/v1.0/deviceManagement/managedDevices?$filter=%s",
		strings.TrimRight(base, "/"),
		url.QueryEscape(filter),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return DeviceStatus{}, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := i.client.Do(req)
	if err != nil {
		return DeviceStatus{}, fmt.Errorf("intune: graph call: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusForbidden {
		return DeviceStatus{}, fmt.Errorf("intune: 403 from Graph — check that the app registration has DeviceManagementManagedDevices.Read.All with admin consent")
	}
	if resp.StatusCode != http.StatusOK {
		return DeviceStatus{}, fmt.Errorf("intune: graph returned %d", resp.StatusCode)
	}

	var body graphResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return DeviceStatus{}, fmt.Errorf("intune: decode response: %w", err)
	}

	if len(body.Value) == 0 {
		return DeviceStatus{
			Found:     false,
			Compliant: false,
			Reason:    fmt.Sprintf("device '%s' not enrolled in Intune", deviceName),
		}, nil
	}

	// Pick the first match. If multiple devices share the hostname,
	// operators see the most-recently-reported one (Graph's default
	// order). Future enhancement: pick the most-recently-synced.
	dev := body.Value[0]
	switch strings.ToLower(dev.ComplianceState) {
	case "compliant", "ingraceperiod":
		return DeviceStatus{
			Found:     true,
			Compliant: true,
			Reason:    "compliant",
		}, nil
	default:
		return DeviceStatus{
			Found:     true,
			Compliant: false,
			Reason: fmt.Sprintf(
				"complianceState=%q, managementState=%q",
				dev.ComplianceState, dev.ManagementState,
			),
		}, nil
	}
}

// escapeFilterValue handles the OData filter quoting rule: a literal
// single quote is escaped by doubling it.
func escapeFilterValue(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
