// dev-seed-flow-events seeds the local dev Postgres with synthetic
// flow events so a developer can preview the Network Traffic timeline
// in the dashboard without needing a full peer mesh up.
//
// Usage:
//
//	go run ./scripts/dev-seed-flow-events
//
// Honors OPENZRO_FLOW_STORE_DSN if set; otherwise falls back to the
// local dev compose default (`make dev.deps.up`):
//
//	host=localhost port=5432 dbname=openzro_flow user=openzro password=openzro sslmode=disable
//
// Idempotent — safe to re-run; each invocation appends a fresh batch
// (the dashboard sorts by received_at desc so newest land on top).
//
// Note: peer enrichment in the dashboard depends on `/api/peers`
// returning matching mesh IPs. If the local management has no peers
// registered, the timeline falls back to bare-IP rendering. That's
// fine for layout preview — once deployed against a real cluster
// the enrichment kicks in automatically.
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	_ "github.com/glebarez/go-sqlite" // pure-go sqlite driver for reading the management data store
	_ "github.com/lib/pq"              // database/sql driver for the postgres admin connection used by ensureDatabase
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/openzro/openzro/flow/store"
	flowsql "github.com/openzro/openzro/flow/store/sql"
)

const defaultDSN = "host=localhost port=5432 dbname=openzro_flow user=openzro password=openzro sslmode=disable"

// defaultMgmtStorePath is where the dev Makefile parks the management
// daemon's sqlite data store. We probe it to discover the operator's
// account ID so seeded events show up under their login session — the
// /api/network-traffic-events handler scopes by AccountID = auth, so
// inserting events under any other ID makes them invisible to the
// dashboard even though they're persisted in Postgres.
const defaultMgmtStorePath = "/tmp/openzro-mgmt-data/store.db"

// errNoAccount fires when the dev management store has no account row
// yet — i.e. the operator has not logged into the dashboard once.
// Treated as a soft skip in main(): a fresh checkout shouldn't fail
// the whole `make dev.dashboard` chain just because there's nothing
// to seed yet.
var errNoAccount = errors.New("dev seed: no account in management store")

// fakePeer is a synthetic peer used to populate source/dest IPs and
// peer_id columns. Names are intentionally suggestive of typical
// home/office personas so the rendered timeline reads naturally.
type fakePeer struct {
	id      string
	name    string
	ip      string
	country string
	osLabel string
}

var fakePeers = []fakePeer{
	{id: "peer-alice", name: "alice-mac", ip: "100.65.0.10", country: "DE", osLabel: "darwin"},
	{id: "peer-bob", name: "bob-win", ip: "100.65.0.20", country: "BR", osLabel: "windows"},
	{id: "peer-carol", name: "carol-linux", ip: "100.65.0.30", country: "US", osLabel: "linux"},
	{id: "peer-server", name: "prod-bastion", ip: "100.65.0.40", country: "IE", osLabel: "linux"},
}

// fakePolicy mirrors the Policy + PolicyRule pair the management
// owns. The ID is a stable hex string so re-running the seed (which
// uses INSERT OR IGNORE) keeps the existing rows. Events reference
// the rule via RuleID = hex.DecodeString(id) so the API output's
// hex.EncodeToString round-trips back to this same value, matching
// the dashboard's policyByID lookup.
type fakePolicy struct {
	id          string
	name        string
	description string
	action      string
	protocol    string
}

var fakePolicies = []fakePolicy{
	{
		id:          "0102030405060708090a0b0c0d0e0f10",
		name:        "Allow Mesh Traffic",
		description: "Default policy allowing connections between mesh peers",
		action:      "accept",
		protocol:    "all",
	},
	{
		id:          "1112131415161718191a1b1c1d1e1f20",
		name:        "Block Database Access",
		description: "Deny direct database connections from non-DB peers",
		action:      "drop",
		protocol:    "tcp",
	},
}

func main() {
	dsn := os.Getenv("OPENZRO_FLOW_STORE_DSN")
	if dsn == "" {
		dsn = defaultDSN
	}

	if err := ensureDatabase(dsn); err != nil {
		log.Fatalf("ensure database: %v", err)
	}

	accountID, err := resolveAccountID()
	if err != nil {
		// First-run fresh-checkout case: management is up, but no
		// operator has logged into the dashboard yet, so no account
		// exists to attach seed data to. Exit cleanly so a parent
		// `make dev.dashboard` doesn't bail out before the dashboard
		// even renders. The user logs in once, then `make dev.seed.flow-events`
		// (or any subsequent `make dev.dashboard`) picks the new
		// account up automatically.
		if errors.Is(err, errNoAccount) {
			log.Printf("dev seed: %v — skipping (log in once, then re-run for synthetic events)", err)
			return
		}
		log.Fatalf("resolve account id: %v", err)
	}

	if err := seedPeers(accountID); err != nil {
		log.Fatalf("seed peers: %v", err)
	}

	if err := seedPolicies(accountID); err != nil {
		log.Fatalf("seed policies: %v", err)
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("open postgres: %v", err)
	}

	s, err := flowsql.New(db)
	if err != nil {
		log.Fatalf("flow store init: %v", err)
	}
	defer s.Close()

	events := generate(accountID)
	if err := s.Save(context.Background(), events); err != nil {
		log.Fatalf("save: %v", err)
	}
	fmt.Printf("✓ seeded %d flow events under account %s\n", len(events), accountID)
	fmt.Println("  open http://localhost:3000/events/network-traffic to preview")
}

// seedPeers inserts the synthetic fakePeers into the management's
// sqlite peers table so /api/peers returns them and the dashboard
// resolver can enrich the seeded flow events with names, OS icons
// and country flags. Idempotent — uses INSERT OR IGNORE keyed on the
// unique (account_id, ip) index, so re-runs after a fresh login do
// not produce duplicate-key errors.
//
// Real deployments don't need this — peers register themselves via
// the WireGuard handshake. We only fake them in dev because no real
// openZro client is connected to the dev management.
func seedPeers(accountID string) error {
	path := os.Getenv("OPENZRO_DEV_MGMT_STORE")
	if path == "" {
		path = defaultMgmtStorePath
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("open management store: %w", err)
	}
	defer db.Close()

	now := time.Now().UTC().Format("2006-01-02 15:04:05.000-07:00")
	// Several Peer fields use gorm:"serializer:json" — IP, ConnectionIP,
	// ExtraDNSLabels, the meta_* (Environment / Flags / Files /
	// NetworkAddresses) blocks. GORM tries to json-decode whatever
	// sqlite returns from those columns, so NULL or raw strings make
	// it fail at /api/peers fetch time with a 500. We pre-encode every
	// json-serialized field below so the round-trip stays clean.
	stmt := `INSERT OR IGNORE INTO peers (
		id, account_id, key, ip, name, dns_label,
		meta_hostname, meta_go_os, meta_os, meta_os_version, meta_kernel,
		meta_core, meta_platform, meta_kernel_version,
		meta_wt_version, meta_ui_version,
		meta_system_serial_number, meta_system_product_name, meta_system_manufacturer,
		meta_network_addresses, meta_environment, meta_flags, meta_files,
		location_country_code, location_city_name, location_connection_ip,
		peer_status_connected, peer_status_last_seen,
		peer_status_login_expired, peer_status_requires_approval,
		ssh_enabled, ssh_key,
		login_expiration_enabled, inactivity_expiration_enabled,
		last_login, created_at,
		ephemeral, allow_extra_dns_labels, extra_dns_labels
	) VALUES (?, ?, ?, ?, ?, ?,  ?, ?, ?, ?, ?,  ?, ?, ?,  ?, ?,  ?, ?, ?,  ?, ?, ?, ?,  ?, ?, ?,  ?, ?,  ?, ?,  ?, ?,  ?, ?,  ?, ?,  ?, ?, ?)`

	created := 0
	for _, p := range fakePeers {
		// Use the same sha256 we put in the flow events as the WG public
		// key surrogate — keeps the dev data internally consistent.
		key := fmt.Sprintf("%x", derivePubkey(p.id))
		ipJSON := fmt.Sprintf("%q", p.ip)
		loopbackJSON := `"127.0.0.1"`
		emptyArr := "[]"
		emptyObj := "{}"
		res, err := db.Exec(stmt,
			p.id, accountID, key, ipJSON, p.name, p.name,
			p.name, p.osLabel, p.osLabel, "1.0", "dev",
			"", p.osLabel, "5.10",
			"0.53.1-dev", "0.53.1-dev",
			"FAKE-SN", "Dev Box", "openZro",
			emptyArr, emptyObj, emptyObj, emptyArr,
			p.country, "Dev City", loopbackJSON,
			1, now,
			0, 0,
			0, "",
			0, 0,
			now, now,
			0, 0, emptyArr,
		)
		if err != nil {
			return fmt.Errorf("insert peer %s: %w", p.name, err)
		}
		n, _ := res.RowsAffected()
		if n > 0 {
			created++
		}
	}
	if created == 0 {
		fmt.Printf("✓ %d fake peers already present\n", len(fakePeers))
	} else {
		fmt.Printf("✓ inserted %d fake peers\n", created)
	}
	return nil
}

// resolveAccountID reads the management daemon's sqlite data store
// and returns the first (and in single-account mode, only) account.
// Honors OPENZRO_DEV_MGMT_STORE for non-default datadir paths. Errors
// out early with a useful hint if the file is missing or empty —
// usually because the operator hasn't logged into the dev dashboard
// yet to trigger account provisioning.
func resolveAccountID() (string, error) {
	path := os.Getenv("OPENZRO_DEV_MGMT_STORE")
	if path == "" {
		path = defaultMgmtStorePath
	}
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("management store %s not found: %w", path, err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return "", fmt.Errorf("open management store: %w", err)
	}
	defer db.Close()

	var id string
	row := db.QueryRow("SELECT id FROM accounts ORDER BY created_at LIMIT 1")
	if err := row.Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("no accounts in %s — log into the dev dashboard first: %w", path, errNoAccount)
		}
		return "", fmt.Errorf("query accounts: %w", err)
	}
	return id, nil
}

// ensureDatabase creates the openzro_flow database if missing. The
// DSN may target an existing DB (idempotent path) so we connect to
// the parent admin DB to issue CREATE DATABASE conditionally — same
// pattern the production chart's pre-install Job uses.
func ensureDatabase(dsn string) error {
	dbName := extractDBName(dsn)
	if dbName == "" {
		return fmt.Errorf("could not extract dbname from DSN")
	}

	adminDSN := strings.Replace(dsn, "dbname="+dbName, "dbname=postgres", 1)
	admin, err := sql.Open("postgres", adminDSN)
	if err != nil {
		// pure-go pq isn't imported here; fall through silently and
		// let the gorm.Open below fail with a clearer message if the
		// DB really doesn't exist.
		return nil
	}
	defer admin.Close()

	var exists bool
	row := admin.QueryRow("SELECT 1 FROM pg_database WHERE datname = $1", dbName)
	if err := row.Scan(&exists); err == sql.ErrNoRows {
		_, err := admin.Exec(fmt.Sprintf("CREATE DATABASE %q", dbName))
		if err != nil {
			return fmt.Errorf("create database %q: %w", dbName, err)
		}
		fmt.Printf("✓ created database %q\n", dbName)
	} else if err != nil {
		return fmt.Errorf("check database existence: %w", err)
	}
	return nil
}

func extractDBName(dsn string) string {
	for _, kv := range strings.Fields(dsn) {
		if strings.HasPrefix(kv, "dbname=") {
			return strings.TrimPrefix(kv, "dbname=")
		}
	}
	return ""
}

// seedPolicies inserts the synthetic Policies + PolicyRules into the
// management's sqlite so /api/policies returns them. The dashboard's
// resolver in NetworkTrafficTimeline.tsx keys by policy.id, and our
// flow events carry RuleID = hex.DecodeString(policy.id) so the API
// round-trip (hex.EncodeToString) lands the same string. Result: the
// Network Traffic timeline renders "Policy <name> allowed/blocked
// the connection" correctly in dev preview.
//
// Idempotent — INSERT OR IGNORE keyed on the primary key.
func seedPolicies(accountID string) error {
	path := os.Getenv("OPENZRO_DEV_MGMT_STORE")
	if path == "" {
		path = defaultMgmtStorePath
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("open management store: %w", err)
	}
	defer db.Close()

	emptyArr := "[]"
	emptyObj := "{}"

	// Need at least one group for sources/destinations to be valid in
	// the management's policy view; we don't actually use it for
	// matching since the events carry pre-resolved rule IDs.
	const groupID = "dev-all-peers"
	if _, err := db.Exec(
		`INSERT OR IGNORE INTO groups (id, account_id, name) VALUES (?, ?, ?)`,
		groupID, accountID, "All Peers",
	); err != nil {
		// groups table schema may differ across upstream snapshots; not
		// fatal — policies still render their name even if rules don't.
		log.Printf("note: skip insert groups (%v); policies will still seed", err)
	}

	createdP, createdR := 0, 0
	for _, fp := range fakePolicies {
		res, err := db.Exec(`INSERT OR IGNORE INTO policies (
			id, account_id, name, description, enabled, source_posture_checks
		) VALUES (?, ?, ?, ?, ?, ?)`,
			fp.id, accountID, fp.name, fp.description, 1, emptyArr,
		)
		if err != nil {
			return fmt.Errorf("insert policy %s: %w", fp.name, err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			createdP++
		}

		// PolicyRule.ID has its own column. Use the same hex (with a
		// `-rule` suffix swapped into the last bytes) so a sibling tool
		// could distinguish them. The dashboard ignores rule IDs and
		// keys off the parent policy.id directly.
		ruleID := fp.id
		groupsJSON := fmt.Sprintf("[%q]", groupID)
		res2, err := db.Exec(`INSERT OR IGNORE INTO policy_rules (
			id, policy_id, name, description, enabled,
			action, destinations, destination_resource, sources, source_resource,
			bidirectional, protocol, ports, port_ranges
		) VALUES (?, ?, ?, ?, ?,  ?, ?, ?, ?, ?,  ?, ?, ?, ?)`,
			ruleID, fp.id, fp.name+" rule", fp.description, 1,
			fp.action, groupsJSON, emptyObj, groupsJSON, emptyObj,
			1, fp.protocol, emptyArr, emptyArr,
		)
		if err != nil {
			return fmt.Errorf("insert policy_rule %s: %w", fp.name, err)
		}
		if n, _ := res2.RowsAffected(); n > 0 {
			createdR++
		}
	}
	if createdP == 0 && createdR == 0 {
		fmt.Printf("✓ %d fake policies already present\n", len(fakePolicies))
	} else {
		fmt.Printf("✓ inserted %d policies + %d rules\n", createdP, createdR)
	}
	return nil
}

// policyBytesFor returns the raw RuleID bytes the events carry for a
// given policy index. The dashboard receives hex.EncodeToString(bytes)
// as event.rule_id and looks it up against the matching policy.id.
func policyBytesFor(idx int) []byte {
	b, err := hex.DecodeString(fakePolicies[idx].id)
	if err != nil {
		// fakePolicies IDs are compile-time constants; a decode failure
		// means a typo in the constant, not a runtime issue.
		panic(fmt.Sprintf("fakePolicy[%d].id is not valid hex: %v", idx, err))
	}
	return b
}

// generate produces a deterministic-shaped batch of synthetic events:
// each "scenario" creates a flow_id with a start event followed by
// either an end (allowed) or a drop (blocked). Mix of TCP, UDP, and
// ICMP keeps the protocol column varied. Spread across the last hour
// so the timestamps look fresh.
func generate(accountID string) []*store.Event {
	now := time.Now().UTC()
	out := make([]*store.Event, 0, 60)

	// Two policy presets — fakePolicies[0] = "Allow Mesh Traffic"
	// (referenced by every accept scenario), fakePolicies[1] =
	// "Block Database Access" (referenced by the postgres drop).
	scenarios := []struct {
		src, dst  *fakePeer
		reporter  *fakePeer
		direction store.Direction
		protocol  uint16
		srcPort   uint32
		dstPort   uint32
		icmpType  uint16
		blocked   bool
		bytesEach uint64
		policyIdx int
	}{
		// alice → server SSH (TCP/22)
		{src: &fakePeers[0], dst: &fakePeers[3], reporter: &fakePeers[3], direction: store.DirectionIngress, protocol: 6, srcPort: 51322, dstPort: 22, bytesEach: 12 * 1024, policyIdx: 0},
		// bob → server HTTPS (TCP/443)
		{src: &fakePeers[1], dst: &fakePeers[3], reporter: &fakePeers[3], direction: store.DirectionIngress, protocol: 6, srcPort: 49901, dstPort: 443, bytesEach: 220 * 1024, policyIdx: 0},
		// carol → alice ping (ICMP echo)
		{src: &fakePeers[2], dst: &fakePeers[0], reporter: &fakePeers[0], direction: store.DirectionIngress, protocol: 1, icmpType: 8, bytesEach: 256, policyIdx: 0},
		// alice → carol ping reply (egress on alice)
		{src: &fakePeers[0], dst: &fakePeers[2], reporter: &fakePeers[0], direction: store.DirectionEgress, protocol: 1, icmpType: 0, bytesEach: 256, policyIdx: 0},
		// bob → carol DNS (UDP/53)
		{src: &fakePeers[1], dst: &fakePeers[2], reporter: &fakePeers[2], direction: store.DirectionIngress, protocol: 17, srcPort: 38221, dstPort: 53, bytesEach: 768, policyIdx: 0},
		// blocked: alice → server postgres (TCP/5432) — policy denies
		{src: &fakePeers[0], dst: &fakePeers[3], reporter: &fakePeers[3], direction: store.DirectionIngress, protocol: 6, srcPort: 50100, dstPort: 5432, blocked: true, bytesEach: 0, policyIdx: 1},
		// long-running: bob → server SSH session
		{src: &fakePeers[1], dst: &fakePeers[3], reporter: &fakePeers[3], direction: store.DirectionIngress, protocol: 6, srcPort: 49823, dstPort: 22, bytesEach: 4 * 1024 * 1024, policyIdx: 0},
	}

	for i, sc := range scenarios {
		// Spread starts across the last hour, oldest first.
		start := now.Add(-time.Duration(60-i*8) * time.Minute)
		flowID := randBytes(32)
		eventStart := randBytes(32)
		eventEnd := randBytes(32)

		typ := store.EventTypeStart
		if sc.blocked {
			typ = store.EventTypeDrop
		}

		ruleID := policyBytesFor(sc.policyIdx)

		out = append(out, &store.Event{
			EventID:       eventStart,
			FlowID:        flowID,
			PeerPublicKey: derivePubkey(sc.reporter.id),
			IsInitiator:   sc.reporter == sc.src,
			AccountID:     accountID,
			PeerID:        sc.reporter.id,
			OccurredAt:    start,
			ReceivedAt:    start.Add(50 * time.Millisecond),
			Type:          typ,
			Direction:     sc.direction,
			Protocol:      sc.protocol,
			SourceIP:      sc.src.ip,
			DestIP:        sc.dst.ip,
			SourcePort:    sc.srcPort,
			DestPort:      sc.dstPort,
			ICMPType:      sc.icmpType,
			RuleID:        ruleID,
		})

		if sc.blocked {
			continue
		}

		// matching end event for the allowed flows
		end := start.Add(time.Duration(30+i*7) * time.Second)
		out = append(out, &store.Event{
			EventID:       eventEnd,
			FlowID:        flowID,
			PeerPublicKey: derivePubkey(sc.reporter.id),
			IsInitiator:   sc.reporter == sc.src,
			AccountID:     accountID,
			PeerID:        sc.reporter.id,
			OccurredAt:    end,
			ReceivedAt:    end.Add(50 * time.Millisecond),
			Type:          store.EventTypeEnd,
			Direction:     sc.direction,
			Protocol:      sc.protocol,
			SourceIP:      sc.src.ip,
			DestIP:        sc.dst.ip,
			SourcePort:    sc.srcPort,
			DestPort:      sc.dstPort,
			ICMPType:      sc.icmpType,
			RxBytes:       sc.bytesEach,
			TxBytes:       sc.bytesEach / 2,
			RxPackets:     uint64(sc.bytesEach / 1500),
			TxPackets:     uint64(sc.bytesEach / 3000),
			RuleID:        ruleID,
		})
	}

	return out
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

// derivePubkey builds a stable 32-byte "public key" from the peer id
// so re-running the seed doesn't churn the value. Real peers carry a
// Curve25519 pubkey here; in dev a sha256 is good enough.
func derivePubkey(peerID string) []byte {
	h := sha256.Sum256([]byte(peerID))
	return h[:]
}
