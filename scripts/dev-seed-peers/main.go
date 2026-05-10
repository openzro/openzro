// dev-seed-peers seeds the local management's sqlite store with a
// diverse set of synthetic peers + groups so a developer can preview
// the /peers screen — including the v2 redesign at /v2-preview/peers
// — against realistic data shapes without standing up a full mesh.
//
// Usage:
//
//	go run ./scripts/dev-seed-peers
//
// Honors OPENZRO_DEV_MGMT_STORE for non-default datadir paths.
// Idempotent — INSERT OR IGNORE on the unique (account_id, ip) index
// for peers and on the primary key for groups, so re-runs after a
// fresh login are safe.
//
// The dev-seed-flow-events script already inserts 4 minimal peers
// (peer-alice/bob/carol/server) at IPs 100.65.0.10–40 so the Network
// Traffic timeline can enrich its source/dest IPs. This script picks
// IPs starting at 100.65.0.50 to coexist without conflicts. Together
// they give /peers ~16 peers covering the visual dimensions the
// production page exercises:
//
//   - status: connected vs offline (the API is binary; "idle" is a
//     pure UX category the v2 redesign derives from last_seen)
//   - login_expired: triggers the "Login required" badge
//   - approval_required: triggers the "Approval pending" badge + the
//     v2 "Pending" tab
//   - login_expiration_enabled=false: triggers "Expiration disabled"
//   - OS, version, country, city diversity for the cell renderers
//
// Real deployments don't need this — peers register themselves via
// the WireGuard handshake.
package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/glebarez/go-sqlite"
)

const defaultMgmtStorePath = "/tmp/openzro-mgmt-data/store.db"

var errNoAccount = errors.New("dev seed: no account in management store")

// seedPeer carries the minimal shape we INSERT into the peers table.
// The schema has many more columns; the rest get filled with sane
// defaults inside insertPeer below.
type seedPeer struct {
	id, name, ip                            string
	country, city                           string
	osLabel, osVersion, kernel              string
	version                                 string
	serial                                  string
	connected, loginExpired                 bool
	requiresApproval                        bool
	loginExpirationEnabled                  bool
	lastSeen                                time.Time
	groupIDs                                []string
}

// seedGroup is the minimal shape for the groups table.
type seedGroup struct {
	id   string
	name string
}

func main() {
	accountID, err := resolveAccountID()
	if err != nil {
		if errors.Is(err, errNoAccount) {
			log.Printf("dev seed: %v — skipping (log in once, then re-run)", err)
			return
		}
		log.Fatalf("resolve account id: %v", err)
	}

	now := time.Now().UTC()
	peers := buildPeers(now)
	groups := buildGroups()

	if err := insertGroups(accountID, groups, peers); err != nil {
		log.Fatalf("seed groups: %v", err)
	}
	if err := insertPeers(accountID, peers); err != nil {
		log.Fatalf("seed peers: %v", err)
	}

	fmt.Printf("✓ seeded %d peers and %d groups under account %s\n", len(peers), len(groups), accountID)
	fmt.Println("  open http://localhost:3000/peers (or /v2-preview/peers) to preview")
}

func buildGroups() []seedGroup {
	return []seedGroup{
		{id: "grp-all", name: "all"},
		{id: "grp-developers", name: "developers"},
		{id: "grp-designers", name: "designers"},
		{id: "grp-routing-peers-br", name: "routing-peers-br"},
		{id: "grp-production", name: "production"},
		{id: "grp-ci", name: "ci"},
		{id: "grp-deprecated", name: "deprecated"},
	}
}

func buildPeers(now time.Time) []seedPeer {
	return []seedPeer{
		{
			id: "dev-peer-mac-1", name: "kleber-laptop", ip: "100.65.0.50",
			country: "BR", city: "São Paulo",
			osLabel: "darwin", osVersion: "15.3", kernel: "Darwin",
			version: "0.53.1-alpha.50",
			serial:  "C02XL0AAJG5L",
			connected: true, lastSeen: now,
			loginExpirationEnabled: true,
			groupIDs:               []string{"grp-all", "grp-developers"},
		},
		{
			id: "dev-peer-mac-2", name: "andre-mbp", ip: "100.65.0.51",
			country: "BR", city: "São Paulo",
			osLabel: "darwin", osVersion: "14.7", kernel: "Darwin",
			version: "0.53.1-alpha.49",
			serial:  "C02WK0AAJG5L",
			connected: true, lastSeen: now.Add(-2 * time.Minute),
			loginExpirationEnabled: true,
			groupIDs:               []string{"grp-all", "grp-developers"},
		},
		{
			id: "dev-peer-rocky-1", name: "routing-peer-br-1", ip: "100.65.0.52",
			country: "BR", city: "São Paulo",
			osLabel: "linux", osVersion: "9.5", kernel: "Linux",
			version: "0.53.1-alpha.50",
			connected: true, lastSeen: now,
			// expiration disabled — "Expiration disabled" badge
			loginExpirationEnabled: false,
			groupIDs:               []string{"grp-all", "grp-routing-peers-br", "grp-production"},
		},
		{
			id: "dev-peer-rocky-2", name: "routing-peer-br-2", ip: "100.65.0.53",
			country: "BR", city: "São Paulo",
			osLabel: "linux", osVersion: "9.5", kernel: "Linux",
			version: "0.53.1-alpha.50",
			connected: true, lastSeen: now,
			loginExpirationEnabled: false,
			groupIDs:               []string{"grp-all", "grp-routing-peers-br", "grp-production"},
		},
		{
			id: "dev-peer-ubuntu-1", name: "matera-prod-jumphost", ip: "100.65.0.54",
			country: "US", city: "Iowa",
			osLabel: "linux", osVersion: "24.04", kernel: "Linux",
			version: "0.53.1-alpha.48",
			// connected but stale last_seen — falls into v2 "Idle"
			connected: true, lastSeen: now.Add(-12 * time.Minute),
			loginExpirationEnabled: true,
			groupIDs:               []string{"grp-all", "grp-production"},
		},
		{
			id: "dev-peer-ubuntu-2", name: "ci-runner-01", ip: "100.65.0.55",
			country: "US", city: "Iowa",
			osLabel: "linux", osVersion: "24.04", kernel: "Linux",
			version: "0.53.1-alpha.47",
			// offline + login expired — "Login required" badge
			connected: false, lastSeen: now.Add(-1 * time.Hour),
			loginExpired:           true,
			loginExpirationEnabled: true,
			groupIDs:               []string{"grp-all", "grp-ci"},
		},
		{
			id: "dev-peer-win-1", name: "ana-workstation", ip: "100.65.0.56",
			country: "BR", city: "São Paulo",
			osLabel: "windows", osVersion: "11 23H2", kernel: "Windows",
			version: "0.53.1-alpha.50",
			serial:  "WIN-AN12-3456",
			connected: true, lastSeen: now,
			loginExpirationEnabled: true,
			groupIDs:               []string{"grp-all", "grp-developers", "grp-designers"},
		},
		{
			id: "dev-peer-debian-1", name: "old-vpn-gateway", ip: "100.65.0.57",
			country: "DE", city: "Frankfurt",
			osLabel: "linux", osVersion: "11", kernel: "Linux",
			version: "0.53.1-alpha.21",
			// offline + approval pending — Pending tab + "Approval pending" badge
			connected: false, lastSeen: now.Add(-4 * 24 * time.Hour),
			requiresApproval:       true,
			loginExpirationEnabled: true,
			groupIDs:               []string{"grp-all", "grp-deprecated"},
		},
		{
			id: "dev-peer-mac-3", name: "felipe-laptop", ip: "100.65.0.58",
			country: "BR", city: "Rio de Janeiro",
			osLabel: "darwin", osVersion: "15.2", kernel: "Darwin",
			version: "0.53.1-alpha.50",
			serial:  "C02ZL0AAJG5L",
			connected: true, lastSeen: now,
			loginExpirationEnabled: true,
			groupIDs:               []string{"grp-all", "grp-developers"},
		},
		{
			id: "dev-peer-arch-1", name: "dev-server-1", ip: "100.65.0.59",
			country: "US", city: "Oregon",
			osLabel: "linux", osVersion: "rolling", kernel: "Linux",
			version: "0.53.1-alpha.50",
			connected: true, lastSeen: now.Add(-30 * time.Second),
			loginExpirationEnabled: true,
			groupIDs:               []string{"grp-all", "grp-developers"},
		},
		{
			id: "dev-peer-fedora-1", name: "analytics-pipeline", ip: "100.65.0.60",
			country: "IE", city: "Dublin",
			osLabel: "linux", osVersion: "39", kernel: "Linux",
			version: "0.53.1-alpha.50",
			connected: true, lastSeen: now,
			loginExpirationEnabled: false,
			groupIDs:               []string{"grp-all", "grp-production"},
		},
		{
			id: "dev-peer-mac-4", name: "client-poc", ip: "100.65.0.61",
			country: "GB", city: "London",
			osLabel: "darwin", osVersion: "13.5", kernel: "Darwin",
			version: "0.53.1-alpha.30",
			connected: false, lastSeen: now.Add(-2 * 24 * time.Hour),
			loginExpirationEnabled: true,
			groupIDs:               []string{"grp-all"},
		},
	}
}

// resolveAccountID reads the management daemon's sqlite data store
// and returns the first (single-account dev mode) account.
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

func openMgmtStore() (*sql.DB, error) {
	path := os.Getenv("OPENZRO_DEV_MGMT_STORE")
	if path == "" {
		path = defaultMgmtStorePath
	}
	return sql.Open("sqlite", path)
}

// insertPeers writes each seedPeer using INSERT OR IGNORE keyed on
// the (account_id, ip) unique index. JSON-serialized columns
// (ip, location_connection_ip, network_addresses, environment,
// flags, files, extra_dns_labels) are pre-encoded so the GORM
// json serializer round-trips cleanly through /api/peers.
func insertPeers(accountID string, peers []seedPeer) error {
	db, err := openMgmtStore()
	if err != nil {
		return fmt.Errorf("open management store: %w", err)
	}
	defer db.Close()

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

	const tsLayout = "2006-01-02 15:04:05.000-07:00"
	emptyArr := "[]"
	emptyObj := "{}"
	loopbackJSON := `"127.0.0.1"`
	created := 0
	for _, p := range peers {
		serial := p.serial
		if serial == "" {
			serial = "FAKE-SN"
		}
		ipJSON := fmt.Sprintf("%q", p.ip)
		key := fmt.Sprintf("dev-key-%s", p.id)
		productName := "Dev Box"
		if p.osLabel == "darwin" {
			productName = "MacBook (dev)"
		} else if p.osLabel == "windows" {
			productName = "Workstation (dev)"
		}
		res, err := db.Exec(stmt,
			p.id, accountID, key, ipJSON, p.name, p.name,
			p.name, p.osLabel, p.osLabel, p.osVersion, p.kernel,
			"", p.osLabel, p.osVersion,
			p.version, p.version,
			serial, productName, "openZro",
			emptyArr, emptyObj, emptyObj, emptyArr,
			p.country, p.city, loopbackJSON,
			boolToInt(p.connected), p.lastSeen.UTC().Format(tsLayout),
			boolToInt(p.loginExpired), boolToInt(p.requiresApproval),
			0, "",
			boolToInt(p.loginExpirationEnabled), 0,
			p.lastSeen.UTC().Format(tsLayout),
			p.lastSeen.UTC().Format(tsLayout),
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
		fmt.Printf("✓ %d peers already present\n", len(peers))
	} else {
		fmt.Printf("✓ inserted %d peers\n", created)
	}
	return nil
}

// insertGroups writes each seedGroup with the JSON-serialized peer
// membership computed from each peer's groupIDs. INSERT OR IGNORE
// keyed on the primary key (group id) keeps re-runs safe — but note
// this means an existing group's membership is NOT updated on re-run.
// Drop the row manually if a peer was added to a group definition
// after the first seed.
func insertGroups(accountID string, groups []seedGroup, peers []seedPeer) error {
	db, err := openMgmtStore()
	if err != nil {
		return fmt.Errorf("open management store: %w", err)
	}
	defer db.Close()

	membership := map[string][]string{}
	for _, p := range peers {
		for _, g := range p.groupIDs {
			membership[g] = append(membership[g], p.id)
		}
	}

	stmt := `INSERT OR IGNORE INTO ` + "`groups`" + ` (id, account_id, name, issued, peers) VALUES (?, ?, ?, ?, ?)`
	created := 0
	for _, g := range groups {
		ids := membership[g.id]
		if ids == nil {
			ids = []string{}
		}
		peersJSON, err := json.Marshal(ids)
		if err != nil {
			return fmt.Errorf("marshal group %s peers: %w", g.id, err)
		}
		res, err := db.Exec(stmt, g.id, accountID, g.name, "api", string(peersJSON))
		if err != nil {
			return fmt.Errorf("insert group %s: %w", g.name, err)
		}
		n, _ := res.RowsAffected()
		if n > 0 {
			created++
		}
	}
	if created == 0 {
		fmt.Printf("✓ %d groups already present\n", len(groups))
	} else {
		fmt.Printf("✓ inserted %d groups\n", created)
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
