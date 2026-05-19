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
	ID                string `json:"id"`
	DeviceName        string `json:"deviceName"`
	ComplianceState   string `json:"complianceState"`
	ManagementState   string `json:"managementState"`
	OperatingSystem   string `json:"operatingSystem"`
	OSVersion         string `json:"osVersion"`
	UserPrincipalName string `json:"userPrincipalName"`
}

// graphSelect is the projection we pin on every /managedDevices call.
// Reduces Graph response payload (a full Intune device record is ~5KB
// of JSON; we use a handful of fields) and avoids paying for fields
// we don't read. Order doesn't matter to Graph.
const graphSelect = "id,deviceName,complianceState,managementState,operatingSystem,osVersion,userPrincipalName"

// GetDeviceStatus queries Graph for the device described by lookup and
// translates the response into DeviceStatus.
func (i *Intune) GetDeviceStatus(ctx context.Context, lookup DeviceLookup) (DeviceStatus, error) {
	return i.lookupAt(ctx, graphBase, lookup)
}

// lookupAt is GetDeviceStatus parameterised on the Graph base URL —
// tests inject an httptest server here.
//
// Search strategy:
//
//  1. If both hostname and userEmail are known, query with the AND
//     of deviceName + userPrincipalName. Disambiguates the case where
//     the same hostname exists on multiple users' enrolled devices
//     (shared / hand-me-down laptops, renamed devices, etc).
//  2. If the combined filter returns no hits, fall back to a
//     userPrincipalName-only query — but ONLY accept the result when
//     the returned device's operatingSystem matches the requesting
//     peer's OS. Without that guard, a user who owns a single
//     compliant Mac in Intune would have every other personal device
//     (Linux PC, Windows desktop) silently pass posture via UPN
//     match. Covers the renamed-hostname case (same-OS device, same
//     user) without leaking compliance across devices.
//  3. If only hostname is known (peer registered via a setup key with
//     no user attribution), fall back to deviceName-only — same OS
//     guard applies for the same reason (common hostnames like
//     "MacBook-Pro" across orgs).
//
// Combined matches (step 1) skip the OS guard because the deviceName
// already pins the lookup to one specific device row.
func (i *Intune) lookupAt(ctx context.Context, base string, lookup DeviceLookup) (DeviceStatus, error) {
	if lookup.Hostname == "" && lookup.UserEmail == "" {
		return DeviceStatus{}, errors.New("intune: lookup requires hostname or user email")
	}

	if lookup.Hostname != "" && lookup.UserEmail != "" {
		filter := fmt.Sprintf(
			"deviceName eq '%s' and userPrincipalName eq '%s'",
			escapeFilterValue(lookup.Hostname),
			escapeFilterValue(lookup.UserEmail),
		)
		st, found, err := i.queryGraph(ctx, base, filter)
		if err != nil {
			return DeviceStatus{}, err
		}
		if found {
			return i.classify(st, lookup), nil
		}
	}

	if lookup.UserEmail != "" {
		filter := fmt.Sprintf(`userPrincipalName eq '%s'`, escapeFilterValue(lookup.UserEmail))
		st, found, err := i.queryGraph(ctx, base, filter)
		if err != nil {
			return DeviceStatus{}, err
		}
		if found && osMatches(st.OperatingSystem, lookup.OS) {
			return i.classify(st, lookup), nil
		}
	}

	if lookup.Hostname != "" {
		filter := fmt.Sprintf(`deviceName eq '%s'`, escapeFilterValue(lookup.Hostname))
		st, found, err := i.queryGraph(ctx, base, filter)
		if err != nil {
			return DeviceStatus{}, err
		}
		if found && osMatches(st.OperatingSystem, lookup.OS) {
			return i.classify(st, lookup), nil
		}
	}

	return DeviceStatus{
		Found:     false,
		Compliant: false,
		Reason:    fmt.Sprintf("device not enrolled in Intune (hostname=%q, user=%q, os=%q)", lookup.Hostname, lookup.UserEmail, lookup.OS),
	}, nil
}

// queryGraph issues a single $filter against /managedDevices and
// returns the first match. found=false means the OData query
// succeeded but no record matched (legitimate "not enrolled" path);
// the err return is reserved for transport / auth / decode failures.
func (i *Intune) queryGraph(ctx context.Context, base, filter string) (graphDevice, bool, error) {
	target := fmt.Sprintf(
		"%s/v1.0/deviceManagement/managedDevices?$filter=%s&$select=%s",
		strings.TrimRight(base, "/"),
		url.QueryEscape(filter),
		url.QueryEscape(graphSelect),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return graphDevice{}, false, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := i.client.Do(req)
	if err != nil {
		return graphDevice{}, false, fmt.Errorf("intune: graph call: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusForbidden {
		return graphDevice{}, false, fmt.Errorf("intune: 403 from Graph — check that the app registration has DeviceManagementManagedDevices.Read.All with admin consent")
	}
	if resp.StatusCode != http.StatusOK {
		return graphDevice{}, false, fmt.Errorf("intune: graph returned %d", resp.StatusCode)
	}

	var body graphResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return graphDevice{}, false, fmt.Errorf("intune: decode response: %w", err)
	}

	if len(body.Value) == 0 {
		return graphDevice{}, false, nil
	}
	// Pick the first match. If multiple devices match (rare with the
	// combined filter; common with userPrincipalName-only fallback)
	// operators see Graph's default order, which is the most recently
	// reporting device first in practice.
	return body.Value[0], true, nil
}

// classify maps a Graph device record into DeviceStatus, honoring
// the per-config StrictCompliance flag.
func (i *Intune) classify(dev graphDevice, lookup DeviceLookup) DeviceStatus {
	state := strings.ToLower(dev.ComplianceState)
	compliant := state == "compliant"
	if !compliant && state == "ingraceperiod" && !i.cfg.StrictCompliance {
		compliant = true
	}
	if compliant {
		return DeviceStatus{
			Found:     true,
			Compliant: true,
			Reason:    fmt.Sprintf("complianceState=%q", dev.ComplianceState),
		}
	}
	_ = lookup // reserved for future enrichment (e.g. include resolved user/host in Reason)
	return DeviceStatus{
		Found:     true,
		Compliant: false,
		Reason: fmt.Sprintf(
			"complianceState=%q, managementState=%q",
			dev.ComplianceState, dev.ManagementState,
		),
	}
}

// escapeFilterValue handles the OData filter quoting rule: a literal
// single quote is escaped by doubling it.
func escapeFilterValue(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// osMatches reports whether Intune's operatingSystem field on a device
// record is consistent with the peer's reported GOOS. Used by the
// fallback paths (UPN-only, hostname-only) to avoid accepting a
// returned device whose OS differs from the peer that triggered the
// lookup — the classic cross-device leak where a user's compliant
// macOS device in Intune would mark every other personal machine
// (Linux PC, Windows desktop) as compliant via shared UPN.
//
// When peerOS is empty the function is permissive (returns true) so
// legacy callers that never set DeviceLookup.OS keep behaving
// exactly as they did before this check landed.
func osMatches(intuneOS, peerOS string) bool {
	if peerOS == "" {
		return true
	}
	intuneOS = strings.ToLower(strings.TrimSpace(intuneOS))
	peerOS = strings.ToLower(strings.TrimSpace(peerOS))
	switch peerOS {
	case "darwin":
		// macOS variants Intune emits: "macOS", "Mac OS X".
		return strings.Contains(intuneOS, "mac")
	case "linux":
		return strings.Contains(intuneOS, "linux")
	case "windows":
		return strings.Contains(intuneOS, "windows")
	case "ios":
		return intuneOS == "ios" || strings.Contains(intuneOS, "ipados")
	case "android":
		return strings.Contains(intuneOS, "android")
	}
	// Unknown peerOS family — be permissive rather than break a vendor
	// integration on a value we haven't seen before.
	return true
}
