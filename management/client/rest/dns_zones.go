package rest

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/openzro/openzro/management/server/http/api"
)

// DNSZonesAPI is the REST surface for the Custom DNS Zones feature
// (issue #108, ADR-0022).
//
// Consumed primarily by openzro/openzro-operator's NbResource
// controller, which materializes DNS records inside operator-managed
// zones for Kubernetes Services that participate in the mesh.
type DNSZonesAPI struct {
	c *Client
}

// ListZones returns all zones in the authenticated account.
func (a *DNSZonesAPI) ListZones(ctx context.Context) ([]api.DNSZone, error) {
	resp, err := a.c.NewRequest(ctx, "GET", "/api/dns/zones", nil, nil)
	if err != nil {
		return nil, err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	return parseResponse[[]api.DNSZone](resp)
}

// GetZone returns a single zone, with its records embedded.
func (a *DNSZonesAPI) GetZone(ctx context.Context, zoneID string) (api.DNSZone, error) {
	resp, err := a.c.NewRequest(ctx, "GET", "/api/dns/zones/"+zoneID, nil, nil)
	if err != nil {
		return api.DNSZone{}, err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	return parseResponse[api.DNSZone](resp)
}

// CreateZone creates a new zone in the authenticated account.
func (a *DNSZonesAPI) CreateZone(ctx context.Context, req api.DNSZoneRequest) (api.DNSZone, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return api.DNSZone{}, err
	}
	resp, err := a.c.NewRequest(ctx, "POST", "/api/dns/zones", bytes.NewReader(body), nil)
	if err != nil {
		return api.DNSZone{}, err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	return parseResponse[api.DNSZone](resp)
}

// UpdateZone replaces the metadata (name, enabled, search-domain
// flag, distribution groups) of an existing zone. Records are not
// affected; manage them via the record endpoints.
func (a *DNSZonesAPI) UpdateZone(ctx context.Context, zoneID string, req api.DNSZoneRequest) (api.DNSZone, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return api.DNSZone{}, err
	}
	resp, err := a.c.NewRequest(ctx, "PUT", "/api/dns/zones/"+zoneID, bytes.NewReader(body), nil)
	if err != nil {
		return api.DNSZone{}, err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	return parseResponse[api.DNSZone](resp)
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

// ListRecords returns all records under a zone.
func (a *DNSZonesAPI) ListRecords(ctx context.Context, zoneID string) ([]api.DNSRecord, error) {
	resp, err := a.c.NewRequest(ctx, "GET", "/api/dns/zones/"+zoneID+"/records", nil, nil)
	if err != nil {
		return nil, err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	return parseResponse[[]api.DNSRecord](resp)
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

// GetRecord returns a single record from a zone.
func (a *DNSZonesAPI) GetRecord(ctx context.Context, zoneID, recordID string) (api.DNSRecord, error) {
	resp, err := a.c.NewRequest(ctx, "GET", "/api/dns/zones/"+zoneID+"/records/"+recordID, nil, nil)
	if err != nil {
		return api.DNSRecord{}, err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	return parseResponse[api.DNSRecord](resp)
}

// UpdateRecord replaces a record's payload.
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
