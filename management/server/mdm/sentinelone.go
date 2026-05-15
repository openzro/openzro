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

	goversion "github.com/hashicorp/go-version"
)

// SentinelOne is the SentinelOne (EDR) provider. Baseline compliance
// for S1 means: an agent for this hostname exists in the tenant, is
// not decommissioned, is active, and is not infected. On top of that
// baseline the operator can opt into additional gates (threat
// threshold, disk encryption, firewall, console connectivity, a
// minimum agent version, and a check-in recency window) — see
// SentinelOneCompliance.
//
// Authentication: a tenant-level API token sent as
// `Authorization: ApiToken <token>`. Tokens are minted in the S1
// console under Settings → Users → Service Users; the openZro
// service user needs the "Viewer" role at minimum (read-only access
// to agents).
//
// Lookup: GET /web/api/v2.1/agents?computerName=<host>. The S1 API
// supports several alternative match keys (uuid, externalId, mac);
// hostname is the one we have on the peer side.
type SentinelOne struct {
	cfg    SentinelOneConfig
	client *http.Client
}

// NewSentinelOne constructs the driver.
func NewSentinelOne(cfg SentinelOneConfig) (*SentinelOne, error) {
	if cfg.ManagementURL == "" {
		return nil, errors.New("sentinelone: management_url is required")
	}
	if cfg.APIToken == "" {
		return nil, errors.New("sentinelone: api_token is required")
	}
	return &SentinelOne{
		cfg:    cfg,
		client: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (s *SentinelOne) Type() ProviderType { return TypeSentinelOne }
func (s *SentinelOne) Close() error       { return nil }

// s1Resp is the relevant subset of the /agents response.
type s1Resp struct {
	Data []s1Agent `json:"data"`
}

type s1Agent struct {
	UUID                  string `json:"uuid"`
	ComputerName          string `json:"computerName"`
	IsActive              bool   `json:"isActive"`
	IsDecommissioned      bool   `json:"isDecommissioned"`
	Infected              bool   `json:"infected"`
	NetworkStatus         string `json:"networkStatus"`
	AgentVersion          string `json:"agentVersion"`
	LastActiveDate        string `json:"lastActiveDate"`
	OperationalState      string `json:"operationalState"`
	ActiveThreats         int    `json:"activeThreats"`
	EncryptedApplications bool   `json:"encryptedApplications"`
	FirewallEnabled       bool   `json:"firewallEnabled"`
}

func (s *SentinelOne) GetDeviceStatus(ctx context.Context, lookup DeviceLookup) (DeviceStatus, error) {
	// SentinelOne keys devices by `computerName`; the per-user email
	// hint isn't part of the agents query, so we ignore lookup.UserEmail.
	return s.lookupAt(ctx, s.cfg.ManagementURL, lookup.Hostname)
}

func (s *SentinelOne) lookupAt(ctx context.Context, base, hostname string) (DeviceStatus, error) {
	if hostname == "" {
		return DeviceStatus{}, errors.New("sentinelone: hostname is empty")
	}
	target := fmt.Sprintf(
		"%s/web/api/v2.1/agents?computerName=%s",
		strings.TrimRight(base, "/"),
		url.QueryEscape(hostname),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return DeviceStatus{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "ApiToken "+s.cfg.APIToken)

	resp, err := s.client.Do(req)
	if err != nil {
		return DeviceStatus{}, fmt.Errorf("sentinelone: api call: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		// fall through
	case http.StatusUnauthorized:
		return DeviceStatus{}, errors.New("sentinelone: 401 from S1 — check ApiToken validity and the service user's role")
	case http.StatusForbidden:
		return DeviceStatus{}, errors.New("sentinelone: 403 from S1 — service user needs at least Viewer role on the tenant")
	default:
		return DeviceStatus{}, fmt.Errorf("sentinelone: api returned %d", resp.StatusCode)
	}

	var body s1Resp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return DeviceStatus{}, fmt.Errorf("sentinelone: decode response: %w", err)
	}

	if len(body.Data) == 0 {
		return DeviceStatus{
			Found:     false,
			Compliant: false,
			Reason:    fmt.Sprintf("hostname '%s' has no S1 agent registered", hostname),
		}, nil
	}

	a := body.Data[0]
	return statusFromS1Agent(a, s.cfg.Compliance), nil
}

// statusFromS1Agent applies the compliance rules in two tiers.
//
// Tier 1 — always-on baseline (not operator-tunable; this is the
// enforcement every existing SentinelOne provider already has, so
// it must never weaken on upgrade): a decommissioned, infected, or
// inactive agent is non-compliant.
//
// Tier 2 — operator opt-in additive gates from SentinelOneCompliance.
// Each is skipped when unset, so the zero-value config reproduces
// the legacy behavior exactly. Evaluated after the baseline so the
// most fundamental "agent is broken/compromised" reasons win the
// Reason string.
func statusFromS1Agent(a s1Agent, comp SentinelOneCompliance) DeviceStatus {
	block := func(reason string) DeviceStatus {
		return DeviceStatus{Found: true, Compliant: false, Reason: reason}
	}

	// ── Tier 1: baseline ────────────────────────────────────────────
	if a.IsDecommissioned {
		return block("S1 agent is decommissioned")
	}
	if a.Infected {
		return block("S1 reports active infection")
	}
	if !a.IsActive {
		return block(fmt.Sprintf("S1 agent is inactive (operational_state=%s)", a.OperationalState))
	}

	// ── Tier 2: operator opt-in gates ───────────────────────────────
	if comp.MaxActiveThreats != nil && a.ActiveThreats > *comp.MaxActiveThreats {
		return block(fmt.Sprintf("S1: %d active threats exceeds the allowed maximum of %d",
			a.ActiveThreats, *comp.MaxActiveThreats))
	}
	if comp.RequireDiskEncryption && !a.EncryptedApplications {
		return block("S1: disk encryption is not enabled")
	}
	if comp.RequireFirewall && !a.FirewallEnabled {
		return block("S1: host firewall is not enabled")
	}
	if comp.RequireNetworkConnected && !strings.EqualFold(a.NetworkStatus, "connected") {
		return block(fmt.Sprintf("S1: agent not connected to the console (network_status=%s)", a.NetworkStatus))
	}
	if comp.MinAgentVersion != "" {
		ok, reason := agentVersionAtLeast(a.AgentVersion, comp.MinAgentVersion)
		if !ok {
			return block(reason)
		}
	}
	if comp.SyncWindowMinutes > 0 {
		if reason, stale := agentCheckInStale(a.LastActiveDate, comp.SyncWindowMinutes); stale {
			return block(reason)
		}
	}

	return DeviceStatus{
		Found:     true,
		Compliant: true,
		Reason:    "S1: active, no infection, all configured conditions met",
	}
}

// agentVersionAtLeast reports whether the agent's reported version is
// >= the configured floor. A version we cannot parse is treated as
// non-compliant (fail-closed): we cannot prove it meets the floor,
// and silently passing an unparseable version would let a tampered
// or malformed agent slip the gate.
func agentVersionAtLeast(agentVer, minVer string) (bool, string) {
	want, err := goversion.NewVersion(minVer)
	if err != nil {
		// Misconfiguration, not the device's fault. Surface it but
		// fail closed so the operator notices.
		return false, fmt.Sprintf("S1: configured minimum agent version %q is invalid", minVer)
	}
	got, err := goversion.NewVersion(strings.TrimSpace(agentVer))
	if err != nil {
		return false, fmt.Sprintf("S1: agent version %q is unparseable; cannot verify it meets the %s floor", agentVer, minVer)
	}
	if got.LessThan(want) {
		return false, fmt.Sprintf("S1: agent version %s is below the required %s", agentVer, minVer)
	}
	return true, ""
}

// agentCheckInStale reports whether the agent's last check-in is
// older than the window. An empty or unparseable timestamp is
// treated as stale (fail-closed) for the same reason as the version
// gate: we cannot prove recency, so we do not assume it.
func agentCheckInStale(lastActive string, windowMinutes int) (string, bool) {
	if strings.TrimSpace(lastActive) == "" {
		return "S1: agent has no last-active timestamp; cannot verify it is within the sync window", true
	}
	// S1 emits RFC3339 with fractional seconds and a Z suffix.
	t, err := time.Parse(time.RFC3339Nano, lastActive)
	if err != nil {
		if t, err = time.Parse(time.RFC3339, lastActive); err != nil {
			return fmt.Sprintf("S1: agent last-active timestamp %q is unparseable", lastActive), true
		}
	}
	age := time.Since(t)
	window := time.Duration(windowMinutes) * time.Minute
	if age > window {
		return fmt.Sprintf("S1: agent last seen %s ago, exceeds the %dm sync window",
			age.Round(time.Minute), windowMinutes), true
	}
	return "", false
}
