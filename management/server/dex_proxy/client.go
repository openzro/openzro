// Package dex_proxy talks to the Dex IdP over its gRPC management
// API (github.com/dexidp/dex/api/v2). Connectors (the federated
// upstream IdPs visible on /dex/auth as "Sign in with X" buttons)
// are managed here so the dashboard's Settings → Authentication
// Providers tab can CRUD them at runtime — no YAML edits, no
// Dex restart.
//
// Wire model: the management binary holds one *Client at boot,
// dialed with mTLS in production (certs at the paths in
// OPENZRO_DEX_GRPC_*) or plaintext in dev (loopback only). The
// admin REST handler reuses this single client.
//
// See ADR-0006 for the architectural rationale.
package dex_proxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	apiv2 "github.com/dexidp/dex/api/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// Config carries the connection parameters loaded from env vars
// at boot. InsecureNoTLS bypasses mTLS — only for dev where the
// gRPC port is on loopback.
type Config struct {
	Addr           string
	CACertPath     string
	ClientCertPath string
	ClientKeyPath  string
	InsecureNoTLS  bool
}

// FromEnv builds a Config from the OPENZRO_DEX_GRPC_* variables
// that configure.sh emits + the management container env. Returns
// nil + nil when no Addr is set: the operator hasn't wired Dex
// yet, so the gRPC client should not start. Callers treat the
// nil Config as "feature off, fall back gracefully".
func FromEnv() (*Config, error) { //nolint:nilnil // optional dependency: nil config + nil error means "Dex not configured" (see doc above); callers handle cfg == nil as "feature off".
	addr := strings.TrimSpace(os.Getenv("OPENZRO_DEX_GRPC_ADDR"))
	if addr == "" {
		return nil, nil
	}
	cfg := &Config{
		Addr:           addr,
		CACertPath:     os.Getenv("OPENZRO_DEX_GRPC_CA_CERT"),
		ClientCertPath: os.Getenv("OPENZRO_DEX_GRPC_CLIENT_CERT"),
		ClientKeyPath:  os.Getenv("OPENZRO_DEX_GRPC_CLIENT_KEY"),
	}
	// Plaintext path is normally restricted to loopback (dev). Two
	// escape hatches for non-loopback plaintext:
	//   1. OPENZRO_DEX_GRPC_INSECURE=true — explicit operator opt-in,
	//      e.g. lab/smoke clusters where the Dex pod runs in the
	//      same namespace as management with NetworkPolicy gating.
	//      Documented in the helm chart so operators don't have to
	//      provision mTLS just to validate a fresh install.
	//   2. mTLS material present (CACert + ClientCert + ClientKey).
	//      Production path; provisioned by the openzro-operator-config
	//      chart via cert-manager.
	insecureOpt := strings.EqualFold(strings.TrimSpace(os.Getenv("OPENZRO_DEX_GRPC_INSECURE")), "true")
	if cfg.CACertPath == "" && cfg.ClientCertPath == "" && cfg.ClientKeyPath == "" {
		if !isLoopbackAddr(addr) && !insecureOpt {
			return nil, fmt.Errorf("dex_proxy: gRPC addr %q is non-loopback but no mTLS certs configured (set OPENZRO_DEX_GRPC_INSECURE=true to opt in to plaintext, or provision OPENZRO_DEX_GRPC_{CA,CLIENT}_CERT)", addr)
		}
		cfg.InsecureNoTLS = true
	}
	return cfg, nil
}

// Client wraps the generated dex.api.v2 client with the lifecycle
// (dial, close) and a small surface tailored to connector CRUD.
// Concurrency: the underlying grpc.ClientConn is goroutine-safe,
// so Client may be reused across requests without extra locking.
type Client struct {
	conn *grpc.ClientConn
	dex  apiv2.DexClient
}

// New dials Dex and returns a ready-to-use Client. The caller
// owns the lifecycle and must Close on shutdown.
func New(cfg Config) (*Client, error) {
	creds, err := buildCreds(cfg)
	if err != nil {
		return nil, err
	}
	conn, err := grpc.NewClient(cfg.Addr, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("dex_proxy: dial %q: %w", cfg.Addr, err)
	}
	return &Client{conn: conn, dex: apiv2.NewDexClient(conn)}, nil
}

// NewWithConn wraps an already-dialed grpc.ClientConn — useful
// for tests that boot an in-memory Dex via bufconn and want to
// exercise the typed wrappers without going through TLS dial.
// Production callers should use New, which owns the connection
// lifecycle.
func NewWithConn(conn *grpc.ClientConn) *Client {
	return &Client{conn: conn, dex: apiv2.NewDexClient(conn)}
}

// Close releases the gRPC connection. Idempotent.
func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func buildCreds(cfg Config) (credentials.TransportCredentials, error) {
	if cfg.InsecureNoTLS {
		return insecure.NewCredentials(), nil
	}
	if cfg.CACertPath == "" || cfg.ClientCertPath == "" || cfg.ClientKeyPath == "" {
		return nil, errors.New("dex_proxy: mTLS requires ca/client.crt/client.key paths")
	}
	caBytes, err := os.ReadFile(cfg.CACertPath)
	if err != nil {
		return nil, fmt.Errorf("dex_proxy: read CA: %w", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caBytes) {
		return nil, errors.New("dex_proxy: ca.crt is not valid PEM")
	}
	cert, err := tls.LoadX509KeyPair(cfg.ClientCertPath, cfg.ClientKeyPath)
	if err != nil {
		return nil, fmt.Errorf("dex_proxy: load client cert: %w", err)
	}
	// ServerName must match the dex server cert's SAN. configure.sh
	// generates SAN=DNS:dex,DNS:localhost,IP:127.0.0.1 so both
	// docker (host=dex) and host-port-forward (host=localhost) work.
	host := serverNameFromAddr(cfg.Addr)
	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caPool,
		ServerName:   host,
		MinVersion:   tls.VersionTLS12,
	}), nil
}

func serverNameFromAddr(addr string) string {
	host := addr
	if idx := strings.LastIndex(addr, ":"); idx > 0 {
		host = addr[:idx]
	}
	return host
}

func isLoopbackAddr(addr string) bool {
	host := serverNameFromAddr(addr)
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// --- typed wrappers around Dex's connector RPCs ----------------

// Connector is the wire-shape openZro's REST handler renders to
// the dashboard. Type is the connector kind ("google", "github",
// "microsoft", "oidc", "ldap", …) — full list at
// https://dexidp.io/docs/connectors/. Name is operator-friendly
// (e.g. "Acme corporate Google"). Config is the per-type JSON
// blob (clientID/clientSecret for OAuth-style; bindDN/userSearch
// for LDAP; etc.).
type Connector struct {
	ID     string
	Type   string
	Name   string
	Config []byte
}

// ListConnectors returns every connector currently in Dex's
// storage backend. Order is whatever Dex returns; the admin UI
// can re-sort by Name if needed.
func (c *Client) ListConnectors(ctx context.Context) ([]Connector, error) {
	resp, err := c.dex.ListConnectors(ctx, &apiv2.ListConnectorReq{})
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]Connector, 0, len(resp.GetConnectors()))
	for _, p := range resp.GetConnectors() {
		out = append(out, Connector{
			ID:     p.GetId(),
			Type:   p.GetType(),
			Name:   p.GetName(),
			Config: p.GetConfig(),
		})
	}
	return out, nil
}

// CreateConnector adds a connector to Dex's storage. Returns
// ErrAlreadyExists when a connector with the same ID already
// lives there. ID, Type and Config must all be non-empty.
func (c *Client) CreateConnector(ctx context.Context, in Connector) error {
	if in.ID == "" || in.Type == "" {
		return errors.New("dex_proxy: connector id and type are required")
	}
	resp, err := c.dex.CreateConnector(ctx, &apiv2.CreateConnectorReq{
		Connector: &apiv2.Connector{
			Id:     in.ID,
			Type:   in.Type,
			Name:   in.Name,
			Config: in.Config,
		},
	})
	if err != nil {
		return mapErr(err)
	}
	if resp.GetAlreadyExists() {
		return ErrAlreadyExists
	}
	return nil
}

// UpdateConnector replaces an existing connector's type/name/
// config. ID is the lookup key; the other fields are the new
// values. Returns ErrNotFound when no connector matches.
func (c *Client) UpdateConnector(ctx context.Context, in Connector) error {
	if in.ID == "" {
		return errors.New("dex_proxy: connector id is required for update")
	}
	resp, err := c.dex.UpdateConnector(ctx, &apiv2.UpdateConnectorReq{
		Id:        in.ID,
		NewType:   in.Type,
		NewName:   in.Name,
		NewConfig: in.Config,
	})
	if err != nil {
		return mapErr(err)
	}
	if resp.GetNotFound() {
		return ErrNotFound
	}
	return nil
}

// DeleteConnector removes a connector by ID. Returns ErrNotFound
// when no connector matches.
func (c *Client) DeleteConnector(ctx context.Context, id string) error {
	if id == "" {
		return errors.New("dex_proxy: connector id is required")
	}
	resp, err := c.dex.DeleteConnector(ctx, &apiv2.DeleteConnectorReq{Id: id})
	if err != nil {
		return mapErr(err)
	}
	if resp.GetNotFound() {
		return ErrNotFound
	}
	return nil
}

// Sentinel errors. The REST handler maps them to HTTP statuses;
// callers can use errors.Is to distinguish.
var (
	ErrAlreadyExists = errors.New("dex_proxy: connector already exists")
	ErrNotFound      = errors.New("dex_proxy: connector not found")
)

// mapErr translates gRPC status codes into our sentinel errors
// where applicable, leaving the original error otherwise.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if s, ok := status.FromError(err); ok {
		switch s.Code() {
		case codes.AlreadyExists:
			return ErrAlreadyExists
		case codes.NotFound:
			return ErrNotFound
		}
	}
	return err
}

// HealthCheck dials Dex with a short timeout to verify the
// connection is live. Used at boot so management can fail fast
// when its Dex peer is unreachable, rather than discovering it
// at the first admin API call.
func (c *Client) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := c.dex.GetVersion(ctx, &apiv2.VersionReq{})
	return err
}
