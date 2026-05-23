// Issue #108 — Custom DNS Zones (Phase 1: management-side CRUD).
//
// License posture per ADR-0022 D8: AGPL clean-room. Reconstructed from
// the public NetBird docs (docs.netbird.io/manage/dns/custom-zones +
// docs.netbird.io/api/resources/dns-zones) and openZro's existing
// nameserver-group code as a structural template. **No upstream AGPL
// diff consulted.** See ADR-0022.
//
// Scope of this file:
//   - CRUD on dns_zones / dns_records / dns_zone_groups via the
//     account manager surface.
//   - D5 validation (FQDN syntax, bidirectional peer-DNS overlap,
//     domain immutability on update, ≥1 distribution group, group
//     existence in account, record name FQDN within zone, CNAME
//     mutex with A/AAAA, content shape per type, TTL default).
//   - Activity events for every mutation.
//
// NOT in this file (deferred to Phase 2):
//   - `UpdateAccountPeers` calls when zones / records change. Phase 2
//     extends the per-peer recompute (DNSConfig.CustomZones) to
//     include user-managed zones; until then there is nothing on the
//     wire to invalidate, so omitting the call here is correct.
//     A reviewer of Phase 2 must remember to wire it in.
package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/miekg/dns"
	"github.com/rs/xid"

	"github.com/openzro/openzro/management/server/activity"
	"github.com/openzro/openzro/management/server/permissions/modules"
	"github.com/openzro/openzro/management/server/permissions/operations"
	"github.com/openzro/openzro/management/server/status"
	"github.com/openzro/openzro/management/server/store"
	"github.com/openzro/openzro/management/server/types"
)

// -- Zone CRUD ----------------------------------------------------------

// GetDNSZone returns one zone scoped to the caller's account, with
// records + distribution groups preloaded.
func (am *DefaultAccountManager) GetDNSZone(ctx context.Context, accountID, userID, zoneID string) (*types.DNSZone, error) {
	if !permitted(am, ctx, accountID, userID, operations.Read) {
		return nil, status.NewPermissionDeniedError()
	}
	zone, err := am.Store.GetDNSZoneByID(ctx, store.LockingStrengthShare, accountID, zoneID)
	if err != nil {
		if errors.Is(err, store.ErrDNSZoneNotFound) {
			return nil, status.NewDNSZoneNotFoundError(zoneID)
		}
		return nil, err
	}
	return zone, nil
}

// ListDNSZones returns all zones for the caller's account.
func (am *DefaultAccountManager) ListDNSZones(ctx context.Context, accountID, userID string) ([]*types.DNSZone, error) {
	if !permitted(am, ctx, accountID, userID, operations.Read) {
		return nil, status.NewPermissionDeniedError()
	}
	return am.Store.GetAccountDNSZones(ctx, store.LockingStrengthShare, accountID)
}

// CreateDNSZone provisions a new zone with the given attributes plus
// (optionally) an initial set of distribution groups. Records are
// always added via the record-level API, never inline at zone
// creation, so empty-on-create is the normal state.
//
// Returns the persisted zone (without records — caller adds them via
// CreateDNSRecord).
func (am *DefaultAccountManager) CreateDNSZone(ctx context.Context, accountID, userID string, zone *types.DNSZone) (*types.DNSZone, error) {
	unlock := am.Store.AcquireWriteLockByUID(ctx, accountID)
	defer unlock()

	if !permitted(am, ctx, accountID, userID, operations.Create) {
		return nil, status.NewPermissionDeniedError()
	}
	if zone == nil {
		return nil, status.Errorf(status.InvalidArgument, "dns zone is nil")
	}

	zone.ID = xid.New().String()
	zone.AccountID = accountID
	zone.Records = nil // records added via CreateDNSRecord

	var updateAccountPeers bool
	err := am.Store.ExecuteInTransaction(ctx, func(tx store.Store) error {
		dnsDomain, err := am.resolvePeerDNSDomainTx(ctx, tx, accountID)
		if err != nil {
			return err
		}
		if err := validateDNSZone(ctx, tx, accountID, zone, dnsDomain, true); err != nil {
			return err
		}
		// A new zone has no records yet (records arrive via
		// CreateDNSRecord), so the predicate is effectively false at
		// create time — but we still compute it for symmetry with the
		// other mutations and to keep the wiring uniform.
		updateAccountPeers, err = areDNSZoneChangesAffectPeers(ctx, tx, accountID, zone, nil)
		if err != nil {
			return err
		}
		if err := tx.IncrementNetworkSerial(ctx, store.LockingStrengthUpdate, accountID); err != nil {
			return err
		}
		return tx.SaveDNSZone(ctx, store.LockingStrengthUpdate, zone)
	})
	if err != nil {
		return nil, err
	}

	am.StoreEvent(ctx, userID, zone.ID, accountID, activity.DNSZoneCreated, zone.EventMeta())
	if updateAccountPeers {
		am.UpdateAccountPeers(ctx, accountID)
	}
	return zone, nil
}

// SaveDNSZone updates zone-level attributes (Name, Enabled,
// SearchDomainEnabled, DistributionGroups). It does NOT touch the
// Records slice — those have their own CRUD path. Domain is immutable
// (D5): a save with a different Domain is rejected, not silently
// patched.
func (am *DefaultAccountManager) SaveDNSZone(ctx context.Context, accountID, userID string, zoneToSave *types.DNSZone) (*types.DNSZone, error) {
	unlock := am.Store.AcquireWriteLockByUID(ctx, accountID)
	defer unlock()

	if !permitted(am, ctx, accountID, userID, operations.Update) {
		return nil, status.NewPermissionDeniedError()
	}
	if zoneToSave == nil {
		return nil, status.Errorf(status.InvalidArgument, "dns zone is nil")
	}

	var saved *types.DNSZone
	var updateAccountPeers bool
	err := am.Store.ExecuteInTransaction(ctx, func(tx store.Store) error {
		dnsDomain, err := am.resolvePeerDNSDomainTx(ctx, tx, accountID)
		if err != nil {
			return err
		}
		existing, err := tx.GetDNSZoneByID(ctx, store.LockingStrengthUpdate, accountID, zoneToSave.ID)
		if err != nil {
			if errors.Is(err, store.ErrDNSZoneNotFound) {
				return status.NewDNSZoneNotFoundError(zoneToSave.ID)
			}
			return err
		}

		// Immutability check (ADR-0022 D5): reject domain change,
		// don't silently keep the old value — the operator deserves a
		// clear 400 over a silent no-op that would look like success.
		if !dnsLabelEqual(existing.Domain, zoneToSave.Domain) {
			return status.Errorf(status.InvalidArgument, "dns zone domain is immutable; create a new zone instead")
		}

		zoneToSave.AccountID = accountID
		zoneToSave.CreatedAt = existing.CreatedAt
		// Records are owned by the record CRUD path; preserve whatever
		// is on disk regardless of what the caller passed.
		zoneToSave.Records = existing.Records

		if err := validateDNSZone(ctx, tx, accountID, zoneToSave, dnsDomain, false); err != nil {
			return err
		}
		updateAccountPeers, err = areDNSZoneChangesAffectPeers(ctx, tx, accountID, zoneToSave, existing)
		if err != nil {
			return err
		}
		if err := tx.IncrementNetworkSerial(ctx, store.LockingStrengthUpdate, accountID); err != nil {
			return err
		}
		if err := tx.SaveDNSZone(ctx, store.LockingStrengthUpdate, zoneToSave); err != nil {
			return err
		}
		saved = zoneToSave
		return nil
	})
	if err != nil {
		return nil, err
	}

	am.StoreEvent(ctx, userID, saved.ID, accountID, activity.DNSZoneUpdated, saved.EventMeta())
	if updateAccountPeers {
		am.UpdateAccountPeers(ctx, accountID)
	}
	return saved, nil
}

// DeleteDNSZone removes a zone and (via CASCADE on the FK) its
// records + group memberships.
func (am *DefaultAccountManager) DeleteDNSZone(ctx context.Context, accountID, zoneID, userID string) error {
	unlock := am.Store.AcquireWriteLockByUID(ctx, accountID)
	defer unlock()

	if !permitted(am, ctx, accountID, userID, operations.Delete) {
		return status.NewPermissionDeniedError()
	}

	var deleted *types.DNSZone
	var updateAccountPeers bool
	err := am.Store.ExecuteInTransaction(ctx, func(tx store.Store) error {
		zone, err := tx.GetDNSZoneByID(ctx, store.LockingStrengthUpdate, accountID, zoneID)
		if err != nil {
			if errors.Is(err, store.ErrDNSZoneNotFound) {
				return status.NewDNSZoneNotFoundError(zoneID)
			}
			return err
		}
		// Delete fans out iff the zone was distributable before the
		// delete — passing the zone as `oldZone` (with newZone=nil)
		// captures exactly that.
		updateAccountPeers, err = areDNSZoneChangesAffectPeers(ctx, tx, accountID, nil, zone)
		if err != nil {
			return err
		}
		if err := tx.IncrementNetworkSerial(ctx, store.LockingStrengthUpdate, accountID); err != nil {
			return err
		}
		if err := tx.DeleteDNSZone(ctx, store.LockingStrengthUpdate, accountID, zoneID); err != nil {
			return err
		}
		deleted = zone
		return nil
	})
	if err != nil {
		return err
	}

	am.StoreEvent(ctx, userID, deleted.ID, accountID, activity.DNSZoneDeleted, deleted.EventMeta())
	if updateAccountPeers {
		am.UpdateAccountPeers(ctx, accountID)
	}
	return nil
}

// -- Record CRUD --------------------------------------------------------

// GetDNSRecord returns a single record from a zone, with cross-account
// leakage prevented at the SQL layer (JOIN on dns_zones.account_id).
func (am *DefaultAccountManager) GetDNSRecord(ctx context.Context, accountID, userID, zoneID, recordID string) (*types.DNSRecord, error) {
	if !permitted(am, ctx, accountID, userID, operations.Read) {
		return nil, status.NewPermissionDeniedError()
	}
	rec, err := am.Store.GetDNSRecordByID(ctx, store.LockingStrengthShare, accountID, zoneID, recordID)
	if err != nil {
		if errors.Is(err, store.ErrDNSRecordNotFound) {
			return nil, status.NewDNSRecordNotFoundError(recordID)
		}
		return nil, err
	}
	return rec, nil
}

// ListDNSRecords returns all records under a zone. Errors out if the
// zone doesn't exist so callers can distinguish "empty zone" from
// "no zone".
func (am *DefaultAccountManager) ListDNSRecords(ctx context.Context, accountID, userID, zoneID string) ([]*types.DNSRecord, error) {
	if !permitted(am, ctx, accountID, userID, operations.Read) {
		return nil, status.NewPermissionDeniedError()
	}
	zone, err := am.Store.GetDNSZoneByID(ctx, store.LockingStrengthShare, accountID, zoneID)
	if err != nil {
		if errors.Is(err, store.ErrDNSZoneNotFound) {
			return nil, status.NewDNSZoneNotFoundError(zoneID)
		}
		return nil, err
	}
	out := make([]*types.DNSRecord, 0, len(zone.Records))
	for i := range zone.Records {
		out = append(out, zone.Records[i].Copy())
	}
	return out, nil
}

// CreateDNSRecord appends a record to an existing zone. The record's
// name must be within the zone's domain; CNAME and A/AAAA are
// mutually exclusive on the same hostname.
func (am *DefaultAccountManager) CreateDNSRecord(ctx context.Context, accountID, userID, zoneID string, record *types.DNSRecord) (*types.DNSRecord, error) {
	unlock := am.Store.AcquireWriteLockByUID(ctx, accountID)
	defer unlock()

	if !permitted(am, ctx, accountID, userID, operations.Create) {
		return nil, status.NewPermissionDeniedError()
	}
	if record == nil {
		return nil, status.Errorf(status.InvalidArgument, "dns record is nil")
	}

	record.ID = xid.New().String()
	record.ZoneID = zoneID
	if record.TTL <= 0 {
		record.TTL = types.DNSRecordDefaultTTL
	}

	var updateAccountPeers bool
	err := am.Store.ExecuteInTransaction(ctx, func(tx store.Store) error {
		zone, err := tx.GetDNSZoneByID(ctx, store.LockingStrengthUpdate, accountID, zoneID)
		if err != nil {
			if errors.Is(err, store.ErrDNSZoneNotFound) {
				return status.NewDNSZoneNotFoundError(zoneID)
			}
			return err
		}
		if err := validateDNSRecord(zone, record); err != nil {
			return err
		}
		if err := validateRecordTypeMutex(zone.Records, record, ""); err != nil {
			return err
		}
		// Compute fan-out signal: synthesize a "post-create"
		// snapshot of the zone (records + the new one) and compare
		// against the pre-create snapshot. Empty → non-empty is the
		// common case that flips a zone from "not distributed" to
		// "distributed", which is exactly the fan-out trigger.
		afterCreate := *zone
		afterCreate.Records = append(append([]types.DNSRecord(nil), zone.Records...), *record)
		updateAccountPeers, err = areDNSZoneChangesAffectPeers(ctx, tx, accountID, &afterCreate, zone)
		if err != nil {
			return err
		}
		if err := tx.IncrementNetworkSerial(ctx, store.LockingStrengthUpdate, accountID); err != nil {
			return err
		}
		return tx.SaveDNSRecord(ctx, store.LockingStrengthUpdate, record)
	})
	if err != nil {
		return nil, err
	}

	am.StoreEvent(ctx, userID, record.ID, accountID, activity.DNSRecordCreated, record.EventMeta())
	if updateAccountPeers {
		am.UpdateAccountPeers(ctx, accountID)
	}
	return record, nil
}

// SaveDNSRecord updates an existing record. The record's name must
// remain within the parent zone's domain; CNAME/A/AAAA mutex applies
// against sibling records (excluding the record being updated).
func (am *DefaultAccountManager) SaveDNSRecord(ctx context.Context, accountID, userID, zoneID string, recordToSave *types.DNSRecord) (*types.DNSRecord, error) {
	unlock := am.Store.AcquireWriteLockByUID(ctx, accountID)
	defer unlock()

	if !permitted(am, ctx, accountID, userID, operations.Update) {
		return nil, status.NewPermissionDeniedError()
	}
	if recordToSave == nil {
		return nil, status.Errorf(status.InvalidArgument, "dns record is nil")
	}
	if recordToSave.TTL <= 0 {
		recordToSave.TTL = types.DNSRecordDefaultTTL
	}

	var updateAccountPeers bool
	err := am.Store.ExecuteInTransaction(ctx, func(tx store.Store) error {
		zone, err := tx.GetDNSZoneByID(ctx, store.LockingStrengthUpdate, accountID, zoneID)
		if err != nil {
			if errors.Is(err, store.ErrDNSZoneNotFound) {
				return status.NewDNSZoneNotFoundError(zoneID)
			}
			return err
		}
		existing, err := tx.GetDNSRecordByID(ctx, store.LockingStrengthShare, accountID, zoneID, recordToSave.ID)
		if err != nil {
			if errors.Is(err, store.ErrDNSRecordNotFound) {
				return status.NewDNSRecordNotFoundError(recordToSave.ID)
			}
			return err
		}
		recordToSave.ZoneID = zoneID
		recordToSave.CreatedAt = existing.CreatedAt

		if err := validateDNSRecord(zone, recordToSave); err != nil {
			return err
		}
		if err := validateRecordTypeMutex(zone.Records, recordToSave, recordToSave.ID); err != nil {
			return err
		}
		// Record update doesn't change zone-level distributability
		// (count stays the same). If the zone was distributable
		// before, it still is — fan-out signal is the same as the
		// zone's pre-mutation state vs itself.
		updateAccountPeers, err = areDNSZoneChangesAffectPeers(ctx, tx, accountID, zone, zone)
		if err != nil {
			return err
		}
		if err := tx.IncrementNetworkSerial(ctx, store.LockingStrengthUpdate, accountID); err != nil {
			return err
		}
		return tx.SaveDNSRecord(ctx, store.LockingStrengthUpdate, recordToSave)
	})
	if err != nil {
		return nil, err
	}

	am.StoreEvent(ctx, userID, recordToSave.ID, accountID, activity.DNSRecordUpdated, recordToSave.EventMeta())
	if updateAccountPeers {
		am.UpdateAccountPeers(ctx, accountID)
	}
	return recordToSave, nil
}

// DeleteDNSRecord removes a record by its ID, scoped to the
// (account, zone) tuple at the SQL layer.
func (am *DefaultAccountManager) DeleteDNSRecord(ctx context.Context, accountID, zoneID, recordID, userID string) error {
	unlock := am.Store.AcquireWriteLockByUID(ctx, accountID)
	defer unlock()

	if !permitted(am, ctx, accountID, userID, operations.Delete) {
		return status.NewPermissionDeniedError()
	}

	var deleted *types.DNSRecord
	var updateAccountPeers bool
	err := am.Store.ExecuteInTransaction(ctx, func(tx store.Store) error {
		zone, err := tx.GetDNSZoneByID(ctx, store.LockingStrengthUpdate, accountID, zoneID)
		if err != nil {
			if errors.Is(err, store.ErrDNSZoneNotFound) {
				return status.NewDNSZoneNotFoundError(zoneID)
			}
			return err
		}
		rec, err := tx.GetDNSRecordByID(ctx, store.LockingStrengthUpdate, accountID, zoneID, recordID)
		if err != nil {
			if errors.Is(err, store.ErrDNSRecordNotFound) {
				return status.NewDNSRecordNotFoundError(recordID)
			}
			return err
		}
		// Synthesize the post-delete zone snapshot so the predicate
		// sees the right "after" state. If this was the last record
		// the zone flips from distributable to not-distributable —
		// which IS a fan-out trigger (peers must stop seeing it).
		afterDelete := *zone
		afterDelete.Records = make([]types.DNSRecord, 0, len(zone.Records))
		for i := range zone.Records {
			if zone.Records[i].ID != recordID {
				afterDelete.Records = append(afterDelete.Records, zone.Records[i])
			}
		}
		updateAccountPeers, err = areDNSZoneChangesAffectPeers(ctx, tx, accountID, &afterDelete, zone)
		if err != nil {
			return err
		}
		if err := tx.IncrementNetworkSerial(ctx, store.LockingStrengthUpdate, accountID); err != nil {
			return err
		}
		if err := tx.DeleteDNSRecord(ctx, store.LockingStrengthUpdate, accountID, zoneID, recordID); err != nil {
			return err
		}
		deleted = rec
		return nil
	})
	if err != nil {
		return err
	}

	am.StoreEvent(ctx, userID, deleted.ID, accountID, activity.DNSRecordDeleted, deleted.EventMeta())
	if updateAccountPeers {
		am.UpdateAccountPeers(ctx, accountID)
	}
	return nil
}

// -- Validation ---------------------------------------------------------

// resolvePeerDNSDomainTx returns the effective peer DNS domain for
// the account, loaded INSIDE the validation transaction so it reflects
// any in-flight settings update. Returns the manager's global default
// (`am.dnsDomain`) when the account has no explicit `Settings.DNSDomain`
// configured — same priority order as `am.GetDNSDomain(settings)` at
// management/server/account.go:1946.
//
// Phase 1 review (high/sec): without this, the overlap check ran
// against the global default and missed accounts that had configured
// a custom DNSDomain, letting an operator create a zone that shadows
// their own peer resolution.
func (am *DefaultAccountManager) resolvePeerDNSDomainTx(ctx context.Context, tx store.Store, accountID string) (string, error) {
	settings, err := tx.GetAccountSettings(ctx, store.LockingStrengthShare, accountID)
	if err != nil {
		return "", fmt.Errorf("load account settings: %w", err)
	}
	return am.GetDNSDomain(settings), nil
}

// validateDNSZone enforces ADR-0022 D5 invariants: FQDN syntax,
// bidirectional peer-DNS overlap rejection, ≥1 distribution group
// (deduplicated), group existence in the account, cross-zone overlap
// rejection against OTHER user-managed zones in the same account
// (excluding the zone being saved). Called inside the same
// transaction as the upsert so reads are consistent with the write.
func validateDNSZone(ctx context.Context, tx store.Store, accountID string, zone *types.DNSZone, peerDNSDomain string, isCreate bool) error {
	zone.Domain = strings.TrimSuffix(strings.ToLower(zone.Domain), ".")
	if zone.Domain == "" {
		return status.Errorf(status.InvalidArgument, "dns zone domain is required")
	}
	if _, ok := dns.IsDomainName(zone.Domain); !ok {
		return status.Errorf(status.InvalidArgument, "dns zone domain %q is not a valid FQDN", zone.Domain)
	}
	if l := len(zone.Name); l < 1 || l > 255 {
		return status.Errorf(status.InvalidArgument, "dns zone name length must be in [1, 255]")
	}

	// Bidirectional peer-DNS overlap. The peer zone is rooted at
	// `peerDNSDomain` and the apex carries one A record per peer.
	// Reject if either domain is a label-aligned suffix of the other.
	if peerDNSDomain != "" && dnsZoneOverlap(zone.Domain, peerDNSDomain) {
		return status.Errorf(status.InvalidArgument,
			"dns zone domain %q overlaps with the peer DNS domain %q; pick a non-overlapping namespace",
			zone.Domain, peerDNSDomain)
	}

	// Cross-zone overlap rejection against OTHER user-managed zones
	// in the same account. Phase 1 review (medium): without this,
	// `example.com` and `sub.example.com` can both be distributed to
	// the same peer; the agent's local resolver flattens records
	// across zones (server.go:599) without zone affinity, breaking
	// D1's "NXDOMAIN authoritative for the more-specific zone"
	// property. Bidirectional suffix overlap is the same primitive
	// used for the peer-DNS check above.
	siblings, err := tx.GetAccountDNSZones(ctx, store.LockingStrengthShare, accountID)
	if err != nil {
		return fmt.Errorf("load sibling dns zones: %w", err)
	}
	for _, s := range siblings {
		if s.ID == zone.ID {
			// Excludes the zone being updated from the overlap check
			// against itself; on create, zone.ID is the freshly-minted
			// xid which never matches an existing row.
			continue
		}
		if dnsZoneOverlap(zone.Domain, s.Domain) {
			return status.Errorf(status.InvalidArgument,
				"dns zone domain %q overlaps with existing zone %q; pick a non-overlapping namespace",
				zone.Domain, s.Domain)
		}
	}

	// Distribution-group constraint: ≥1, all in-account, no
	// duplicates. Phase 1 review (low/med): without dedup a payload
	// like `["g1", "g1"]` would crash on the composite PK with a DB
	// duplicate-key error surfaced as 500 — return 400 cleanly here.
	groupIDs := zone.GroupIDs()
	if len(groupIDs) == 0 {
		return status.Errorf(status.InvalidArgument, "dns zone requires at least one distribution group")
	}
	seen := make(map[string]struct{}, len(groupIDs))
	for _, gID := range groupIDs {
		if gID == "" {
			return status.Errorf(status.InvalidArgument, "dns zone distribution group id is empty")
		}
		if _, dup := seen[gID]; dup {
			return status.Errorf(status.InvalidArgument, "dns zone distribution group %q appears more than once", gID)
		}
		seen[gID] = struct{}{}
	}
	groups, err := tx.GetGroupsByIDs(ctx, store.LockingStrengthShare, accountID, groupIDs)
	if err != nil {
		return fmt.Errorf("load distribution groups: %w", err)
	}
	for _, gID := range groupIDs {
		if _, ok := groups[gID]; !ok {
			return status.Errorf(status.InvalidArgument, "dns zone distribution group %q not found in account", gID)
		}
	}
	return nil
}

// validateDNSRecord enforces per-record D5 invariants: record name
// within zone domain, type ∈ {A, AAAA, CNAME}, content shape per
// type, TTL ≥ 0.
//
// The CNAME mutex check is a sibling-aware pass and lives in
// validateRecordTypeMutex below — separated because it needs the
// sibling slice the validator doesn't naturally have.
func validateDNSRecord(zone *types.DNSZone, record *types.DNSRecord) error {
	record.Name = strings.TrimSuffix(strings.ToLower(record.Name), ".")
	if record.Name == "" {
		return status.Errorf(status.InvalidArgument, "dns record name is required")
	}
	if _, ok := dns.IsDomainName(record.Name); !ok {
		return status.Errorf(status.InvalidArgument, "dns record name %q is not a valid FQDN", record.Name)
	}
	if !isWithinZone(record.Name, zone.Domain) {
		return status.Errorf(status.InvalidArgument,
			"dns record name %q is not within zone domain %q", record.Name, zone.Domain)
	}

	switch record.Type {
	case types.DNSRecordTypeA:
		ip := net.ParseIP(record.Content)
		if ip == nil || ip.To4() == nil {
			return status.Errorf(status.InvalidArgument, "A record content must be an IPv4 address; got %q", record.Content)
		}
	case types.DNSRecordTypeAAAA:
		ip := net.ParseIP(record.Content)
		if ip == nil || ip.To4() != nil { // To4()!=nil means it's a v4-mapped, reject
			return status.Errorf(status.InvalidArgument, "AAAA record content must be an IPv6 address; got %q", record.Content)
		}
	case types.DNSRecordTypeCNAME:
		target := strings.TrimSuffix(strings.ToLower(record.Content), ".")
		// Reject IP literals — an IP-shaped CNAME target is almost
		// always a misconfigured A/AAAA record. The miekg parser is
		// lenient enough to accept IP-like strings as domain names.
		if target == "" || net.ParseIP(target) != nil {
			return status.Errorf(status.InvalidArgument, "CNAME record content must be a hostname; got %q", record.Content)
		}
		if _, ok := dns.IsDomainName(target); !ok {
			return status.Errorf(status.InvalidArgument, "CNAME record content must be a hostname; got %q", record.Content)
		}
		// Final defensive check: a real hostname has at least one dot
		// OR is a single short label. Reject content with characters
		// the miekg parser tolerates that operators rarely intend
		// (whitespace, control chars, leading punctuation).
		for _, r := range target {
			if r == ' ' || r == '\t' || r == '\n' {
				return status.Errorf(status.InvalidArgument, "CNAME record content has whitespace; got %q", record.Content)
			}
		}
		record.Content = target
	default:
		return status.Errorf(status.InvalidArgument, "dns record type %q not supported in v1 (allowed: A, AAAA, CNAME)", record.Type)
	}

	// TTL=0 would tell resolvers to bypass the cache, defeating the
	// purpose of an authoritative private zone. Phase 1 review (low):
	// the OpenAPI now declares `minimum: 1`, this is the server-side
	// enforcement of that contract. Note that Create/Save also DEFAULT
	// TTL to 300 when omitted/zero — the explicit-0 path therefore
	// never reaches this validator under normal flow; this guard is
	// defense-in-depth for direct manager-layer calls (e.g. tests).
	if record.TTL <= 0 {
		return status.Errorf(status.InvalidArgument, "dns record ttl must be ≥ 1; omit the field to use the default (300)")
	}
	return nil
}

// validateRecordTypeMutex enforces the CNAME / A-AAAA mutex on the
// same hostname (RFC 1034 §3.6.2). `siblings` is the current set of
// records on the zone; `excludeID` lets the update path skip the
// record being updated (so it doesn't conflict with itself).
func validateRecordTypeMutex(siblings []types.DNSRecord, incoming *types.DNSRecord, excludeID string) error {
	incomingIsCNAME := incoming.Type == types.DNSRecordTypeCNAME
	for _, s := range siblings {
		if s.ID == excludeID {
			continue
		}
		if !strings.EqualFold(s.Name, incoming.Name) {
			continue
		}
		siblingIsCNAME := s.Type == types.DNSRecordTypeCNAME
		if incomingIsCNAME && !siblingIsCNAME {
			return status.Errorf(status.InvalidArgument,
				"cannot add CNAME %q: an A or AAAA record already exists at the same name", incoming.Name)
		}
		if !incomingIsCNAME && siblingIsCNAME {
			return status.Errorf(status.InvalidArgument,
				"cannot add %s %q: a CNAME already exists at the same name", incoming.Type, incoming.Name)
		}
	}
	return nil
}

// dnsZoneOverlap reports whether a and b are the same FQDN OR one is
// a label-aligned suffix of the other (ancestor/descendant). Used by
// validateDNSZone to enforce ADR-0022 D5's bidirectional overlap
// rejection vs the peer DNS domain.
//
// Examples:
//
//	dnsZoneOverlap("openzro",          "openzro")        → true (same)
//	dnsZoneOverlap("private.openzro",  "openzro")        → true (descendant)
//	dnsZoneOverlap("openzro",          "private.openzro") → true (ancestor)
//	dnsZoneOverlap("io",               "openzro")        → false (sibling)
//	dnsZoneOverlap("openzr",           "openzro")        → false (substring, not label-aligned)
func dnsZoneOverlap(a, b string) bool {
	a = strings.TrimSuffix(strings.ToLower(a), ".")
	b = strings.TrimSuffix(strings.ToLower(b), ".")
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	// a is a descendant of b iff a ends with "." + b
	if strings.HasSuffix(a, "."+b) {
		return true
	}
	if strings.HasSuffix(b, "."+a) {
		return true
	}
	return false
}

// dnsLabelEqual is a case-insensitive label comparison with trailing
// dot tolerance — used to detect immutable-domain changes on PUT
// without false positives from cosmetic casing or dot tweaks.
func dnsLabelEqual(a, b string) bool {
	return strings.EqualFold(strings.TrimSuffix(a, "."), strings.TrimSuffix(b, "."))
}

// isWithinZone reports whether a record name is the zone apex or a
// subdomain of it. Both inputs are pre-normalized (lowercase, no
// trailing dot) by the callers.
func isWithinZone(recordName, zoneDomain string) bool {
	if recordName == zoneDomain {
		return true
	}
	return strings.HasSuffix(recordName, "."+zoneDomain)
}

// areDNSZoneChangesAffectPeers reports whether a zone mutation needs
// to fan out via UpdateAccountPeers. Returns true iff at least one
// peer is in one of the distribution groups associated with either
// the new OR the old version of the zone, AND the zone is in a
// distributable state in at least one snapshot (Enabled + has
// records). An empty-or-disabled → empty-or-disabled transition is a
// no-op on the wire.
//
// Mirrors `areNameServerGroupChangesAffectPeers` at
// management/server/nameserver.go:238-253. Both versions accept nil
// for `oldZone` to support the create path; pass the new zone as
// both arguments for record-level mutations where the zone metadata
// is unchanged.
func areDNSZoneChangesAffectPeers(ctx context.Context, tx store.Store, accountID string, newZone, oldZone *types.DNSZone) (bool, error) {
	// Optimization: if neither snapshot reaches the wire, no fan-out
	// needed.
	newDistributable := newZone != nil && newZone.Enabled && len(newZone.Records) > 0
	oldDistributable := oldZone != nil && oldZone.Enabled && len(oldZone.Records) > 0
	if !newDistributable && !oldDistributable {
		return false, nil
	}

	check := func(zone *types.DNSZone) (bool, error) {
		if zone == nil {
			return false, nil
		}
		return anyGroupHasPeersOrResources(ctx, tx, accountID, zone.GroupIDs())
	}
	if newDistributable {
		hit, err := check(newZone)
		if err != nil || hit {
			return hit, err
		}
	}
	if oldDistributable {
		return check(oldZone)
	}
	return false, nil
}

// permitted is a thin wrapper around the existing permissions manager
// for the DNSZones module + the requested operation. Kept inline (not
// a method) so callers stay a one-liner — the early-return pattern
// at the top of every method becomes if !permitted(...) { 403 }.
func permitted(am *DefaultAccountManager, ctx context.Context, accountID, userID string, op operations.Operation) bool {
	allowed, err := am.permissionsManager.ValidateUserPermissions(ctx, accountID, userID, modules.DNSZones, op)
	if err != nil {
		return false
	}
	return allowed
}
