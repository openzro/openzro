package rest

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/openzro/openzro/management/server/http/api"
)

// DNSZonesAPI is the REST surface for the DNS-as-a-service feature.
//
// Currently consumed primarily by openzro/openzro-operator's
// NbResource controller, which materializes DNS records inside
// operator-managed zones for Kubernetes Services that participate
// in the mesh. Server-side implementation (handlers + storage) is
// tracked as a milestone item — see memory file
// project_enterprise_gaps.md "DNS Zones".
type DNSZonesAPI struct {
	c *Client
}

// ListZones returns all zones in the authenticated account.
func (a *DNSZonesAPI) ListZones(ctx context.Context) ([]api.Zone, error) {
	resp, err := a.c.NewRequest(ctx, "GET", "/api/dns/zones", nil, nil)
	if err != nil {
		return nil, err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	return parseResponse[[]api.Zone](resp)
}

// GetZone returns a single zone, with its records embedded.
func (a *DNSZonesAPI) GetZone(ctx context.Context, zoneID string) (api.Zone, error) {
	resp, err := a.c.NewRequest(ctx, "GET", "/api/dns/zones/"+zoneID, nil, nil)
	if err != nil {
		return api.Zone{}, err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	return parseResponse[api.Zone](resp)
}

// CreateZone creates a new zone in the authenticated account.
func (a *DNSZonesAPI) CreateZone(ctx context.Context, req api.ZoneRequest) (api.Zone, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return api.Zone{}, err
	}
	resp, err := a.c.NewRequest(ctx, "POST", "/api/dns/zones", bytes.NewReader(body), nil)
	if err != nil {
		return api.Zone{}, err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	return parseResponse[api.Zone](resp)
}

// UpdateZone replaces the metadata (name, description) of an
// existing zone. Records are not affected.
func (a *DNSZonesAPI) UpdateZone(ctx context.Context, zoneID string, req api.ZoneRequest) (api.Zone, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return api.Zone{}, err
	}
	resp, err := a.c.NewRequest(ctx, "PUT", "/api/dns/zones/"+zoneID, bytes.NewReader(body), nil)
	if err != nil {
		return api.Zone{}, err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	return parseResponse[api.Zone](resp)
}

// DeleteZone removes the zone and all its records.
func (a *DNSZonesAPI) DeleteZone(ctx context.Context, zoneID string) error {
	resp, err := a.c.NewRequest(ctx, "DELETE", "/api/dns/zones/"+zoneID, nil, nil)
	if err != nil {
		return err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	return nil
}

// CreateRecord adds a new record to the given zone.
func (a *DNSZonesAPI) CreateRecord(ctx context.Context, zoneID string, req api.DNSRecordRequest) (api.DNSRecord, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return api.DNSRecord{}, err
	}
	resp, err := a.c.NewRequest(ctx, "POST", "/api/dns/zones/"+zoneID+"/records", bytes.NewReader(body), nil)
	if err != nil {
		return api.DNSRecord{}, err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	return parseResponse[api.DNSRecord](resp)
}

// UpdateRecord replaces a record's payload. Type is treated as
// immutable on update — to change record kind, delete + recreate.
func (a *DNSZonesAPI) UpdateRecord(ctx context.Context, zoneID, recordID string, req api.DNSRecordRequest) (api.DNSRecord, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return api.DNSRecord{}, err
	}
	resp, err := a.c.NewRequest(ctx, "PUT", "/api/dns/zones/"+zoneID+"/records/"+recordID, bytes.NewReader(body), nil)
	if err != nil {
		return api.DNSRecord{}, err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	return parseResponse[api.DNSRecord](resp)
}

// DeleteRecord removes a single record from a zone.
func (a *DNSZonesAPI) DeleteRecord(ctx context.Context, zoneID, recordID string) error {
	resp, err := a.c.NewRequest(ctx, "DELETE", "/api/dns/zones/"+zoneID+"/records/"+recordID, nil, nil)
	if err != nil {
		return err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	return nil
}
