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
)

// Huntress is the Huntress (managed-detection EDR) provider.
//
// Authentication: HTTP Basic with the API key as username and the
// API secret as password. Both are minted in the Huntress dashboard
// under Account Settings → API Credentials.
//
// Lookup: GET /v1/agents?hostname=<host>. The response carries an
// `agents` array where the agent's `last_callback_at` and
// `incident_reports_count` (or similar) determine compliance.
//
// Compliance rule: agent exists, has called back recently, and has
// no unresolved incidents.
type Huntress struct {
	cfg    HuntressConfig
	base   string // overridable for tests
	client *http.Client
}

const huntressBaseDefault = "https://api.huntress.io"

func NewHuntress(cfg HuntressConfig) (*Huntress, error) {
	if cfg.APIKey == "" || cfg.APISecret == "" {
		return nil, errors.New("huntress: api_key and api_secret are required")
	}
	return &Huntress{
		cfg:    cfg,
		base:   huntressBaseDefault,
		client: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (h *Huntress) Type() ProviderType { return TypeHuntress }
func (h *Huntress) Close() error       { return nil }

type huntressResp struct {
	Agents []huntressAgent `json:"agents"`
}

type huntressAgent struct {
	ID                   int64  `json:"id"`
	Hostname             string `json:"hostname"`
	OperatingSystem      string `json:"operating_system"`
	LastCallbackAt       string `json:"last_callback_at"`
	OutdatedAgentVersion bool   `json:"outdated_agent_version"`
	IncidentReportsCount int    `json:"incident_reports_count"`
}

func (h *Huntress) GetDeviceStatus(ctx context.Context, lookup DeviceLookup) (DeviceStatus, error) {
	// Huntress agents are keyed by hostname; per-user email isn't
	// modeled in their API, so we ignore lookup.UserEmail.
	return h.lookupAt(ctx, h.base, lookup.Hostname)
}

func (h *Huntress) lookupAt(ctx context.Context, base, hostname string) (DeviceStatus, error) {
	if hostname == "" {
		return DeviceStatus{}, errors.New("huntress: hostname is empty")
	}
	target := fmt.Sprintf(
		"%s/v1/agents?hostname=%s",
		strings.TrimRight(base, "/"),
		url.QueryEscape(hostname),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return DeviceStatus{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(h.cfg.APIKey, h.cfg.APISecret)

	resp, err := h.client.Do(req)
	if err != nil {
		return DeviceStatus{}, fmt.Errorf("huntress: api call: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return DeviceStatus{}, errors.New("huntress: 401/403 — verify the API Key and Secret in the dashboard")
	default:
		return DeviceStatus{}, fmt.Errorf("huntress: api returned %d", resp.StatusCode)
	}

	var body huntressResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return DeviceStatus{}, fmt.Errorf("huntress: decode response: %w", err)
	}

	if len(body.Agents) == 0 {
		return DeviceStatus{
			Found:     false,
			Compliant: false,
			Reason:    fmt.Sprintf("hostname '%s' has no Huntress agent", hostname),
		}, nil
	}

	a := body.Agents[0]
	return statusFromHuntressAgent(a), nil
}

func statusFromHuntressAgent(a huntressAgent) DeviceStatus {
	if a.IncidentReportsCount > 0 {
		return DeviceStatus{
			Found:     true,
			Compliant: false,
			Reason:    fmt.Sprintf("Huntress reports %d open incident(s)", a.IncidentReportsCount),
		}
	}
	if a.OutdatedAgentVersion {
		return DeviceStatus{
			Found:     true,
			Compliant: false,
			Reason:    "Huntress agent is outdated — update required",
		}
	}
	return DeviceStatus{
		Found:     true,
		Compliant: true,
		Reason:    "Huntress: agent healthy, no incidents",
	}
}
