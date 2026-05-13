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

// SentinelOne is the SentinelOne (EDR) provider. Compliance for S1
// means: an agent for this hostname exists in the tenant, is active,
// and has no unresolved infections.
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
	UUID             string `json:"uuid"`
	ComputerName     string `json:"computerName"`
	IsActive         bool   `json:"isActive"`
	IsDecommissioned bool   `json:"isDecommissioned"`
	Infected         bool   `json:"infected"`
	NetworkStatus    string `json:"networkStatus"`
	AgentVersion     string `json:"agentVersion"`
	LastActiveDate   string `json:"lastActiveDate"`
	OperationalState string `json:"operationalState"`
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
	return statusFromS1Agent(a), nil
}

// statusFromS1Agent applies the compliance rules. Decommissioned or
// infected agents are non-compliant; inactive agents are non-compliant
// too (the agent might be installed but offline, which means nothing
// is actively defending the box).
func statusFromS1Agent(a s1Agent) DeviceStatus {
	if a.IsDecommissioned {
		return DeviceStatus{
			Found:     true,
			Compliant: false,
			Reason:    "S1 agent is decommissioned",
		}
	}
	if a.Infected {
		return DeviceStatus{
			Found:     true,
			Compliant: false,
			Reason:    "S1 reports active infection",
		}
	}
	if !a.IsActive {
		return DeviceStatus{
			Found:     true,
			Compliant: false,
			Reason:    fmt.Sprintf("S1 agent is inactive (operational_state=%s)", a.OperationalState),
		}
	}
	return DeviceStatus{
		Found:     true,
		Compliant: true,
		Reason:    "S1: active, no infection",
	}
}
