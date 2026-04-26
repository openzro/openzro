# ADR-0002: Flow events storage architecture

- **Status**: Accepted
- **Date**: 2026-04-26
- **Decision-makers**: openzro maintainers
- **Supersedes**: —
- **Superseded by**: —

## Context

openZro inherits from NetBird a `FlowService.Events` bidirectional gRPC
stream where peers publish per-connection flow records (start, end,
drop) tagged with src/dst IPs, ports, protocol, byte/packet counts,
and the rule ID that allowed or denied each connection. The proto
shape is BSD-3-licensed and lives at [`flow/proto/flow.proto`](../../flow/proto/flow.proto).

Until commit `08398f9e` no server-side handler was registered: clients
hit `Unimplemented` and either retried tightly or surfaced log noise.
That commit added an ack-only handler so peers stop misbehaving, but
the events themselves are dropped on the floor.

This ADR records the decision for what to do with those events:
where to store them, for how long, and how to expose them to
operators.

### Volume estimate

A single peer with ~100 active connections generates ~10 events/sec
(start + end + drop). Realistic deployments:

| Deployment | Peers | Events/sec | Events/day |
|---|---|---|---|
| Tiny / dev | 5–20 | 50–200 | 5M–20M |
| Small team | 50–200 | 500–2000 | 50M–200M |
| Medium | 500–2000 | 5k–20k | 500M–2B |
| Large | 5000+ | 50k+ | 5B+ |

Postgres without specialization stalls in the medium tier on writes.
Postgres with monthly partitioning + careful indexes holds up through
small-team. ClickHouse holds up through large.

### What NetBird does (per ADR-0001 §3.3 — public sources only)

Public docs ([`docs.netbird.io/manage/activity/traffic-events-logging`](https://docs.netbird.io/manage/activity/traffic-events-logging))
state that traffic events are retained **7 days hard-coded** and that
ingestion latency is "up to 10 minutes" — strongly suggesting an
asynchronous pipeline rather than synchronous DB writes. The storage
backend is **never named publicly** because the feature is cloud-only
in upstream — it does not ship in self-hosted NetBird at all. There
is therefore no upstream precedent for self-host storage to mirror,
and our decisions here are independent.

## Decision

### Three independent destinations, operator picks any subset

```
peer ──gRPC FlowService.Events──> management
                                     │
                                     ├─> HOT store    (queryable from UI)
                                     ├─> SIEM stream  (per-event POST, real-time)
                                     └─> COLD archive (batched Parquet to S3/GCS/R2)
```

Each destination is independent and configured separately. Default
shipped configuration is **HOT only** (Postgres) so a vanilla
self-hosted deployment has a working "Network Traffic Events" page
without external infra.

### HOT tier: pluggable backend, openZro-supported drivers

`flow.Store` is an interface the management server consumes. Two
backends ship in-tree:

  - **`postgres`** — same database family as the management's primary
    store. Uses monthly partitioning via `pg_partman` extension when
    available, falls back to manual partitions when not. Holds the
    "small team" tier comfortably.
  - **`clickhouse`** — columnar; the right tool for medium and larger
    deployments. Single-binary deploy, Apache 2.0 licensed.

Operators select via env var:

```
OPENZRO_FLOW_STORE_ENGINE=none|postgres|clickhouse   # default: none
OPENZRO_FLOW_STORE_DSN=postgres://… or clickhouse://…
OPENZRO_FLOW_RETENTION=168h                          # default: 7 days
```

`engine=none` is supported and means "don't persist; UI shows nothing
historical; rely entirely on SIEM/cold for retention". This is the
deployment shape for operators who already pipe everything to Splunk.

### SIEM streaming: extends the existing exporter

[`management/server/activity/exporter/`](../../management/server/activity/exporter/)
already ships an HTTP webhook exporter for activity events (commit
`c79d5813`). Flow events extend the same machinery:

```
OPENZRO_FLOW_EXPORT_URL=https://siem.example.com/ingest
OPENZRO_FLOW_EXPORT_HEADERS={"Authorization":"Bearer …"}
```

Per-event `POST application/json`, retry on 5xx, drop on 4xx — same
contract as the activity exporter.

**Supported SIEM destinations (out of the box, via Generic HTTP):**

The Generic HTTP exporter is the lowest-common-denominator interface
that covers every SIEM accepting a JSON-bodied POST. With the right
URL + headers, a single exporter implementation reaches:

| Destination | Configuration shape |
|---|---|
| **Datadog Logs Intake** | URL `https://http-intake.logs.<region>.datadoghq.com/api/v2/logs`, headers `{"DD-API-KEY":"…"}` |
| **Splunk HEC** | URL `https://<host>:8088/services/collector/event`, headers `{"Authorization":"Splunk <token>"}` |
| **Sumo Logic HTTP Source** | URL from collector + no auth header |
| **Elastic via filebeat HTTP** | URL pointing at filebeat HTTP input; auth via headers |
| **Grafana Loki** | URL `https://<loki>/loki/api/v1/push`, headers basic auth |
| **SentinelOne Singularity Data Lake** | URL + headers per tenant |
| **Generic webhook (Slack, Discord, custom)** | Any URL accepting JSON POST |

Datadog and Splunk in particular expect specific JSON envelopes
(`ddsource`/`ddtags` for Datadog, `{"event": …}` for Splunk HEC).
The generic exporter sends our event shape directly; for these
targets the operator either:

  1. Configures their SIEM to accept the openZro shape (Datadog
     accepts arbitrary keys, Splunk does too with `EVENT_BREAKER`),
     or
  2. Routes through a proxy that re-shapes the payload (Vector,
     Logstash, an in-house transformer).

**Future payload templates** (deferred PR): add an env-driven Go
template applied to the event before POST, so operators can produce
exactly the JSON shape the destination expects without a proxy. This
is purely additive to the existing exporter — no breaking change.

**Native vendor-specific drivers** (deferred, demand-driven): each
SIEM is one extra file alongside `http.go` implementing the
`Exporter` interface. We do not ship these by default to avoid
maintaining N drivers we cannot test against real corporate
tenants. They land if operators request them with a real deployment
to validate against.

### COLD archive: batched object-storage writes

Per-event POSTs to S3 are wasteful and expensive. The cold path
buffers events in memory and writes a Parquet file every N minutes
(default 15 min) to a configured bucket:

```
OPENZRO_FLOW_ARCHIVE_S3_BUCKET=openzro-flow-archive
OPENZRO_FLOW_ARCHIVE_S3_REGION=us-east-1
OPENZRO_FLOW_ARCHIVE_INTERVAL=15m
```

GCS and R2 use the same interface with different auth glue. Parquet is
chosen because it is the universal format for analytical query tools
(DuckDB, Athena, BigQuery, ClickHouse) — operators querying their
archive with their own tools should not need openzro-specific
decoders.

### Retention and aging

Hot store retention is enforced by a daily purge job (cron-style
inside the management process) that drops partitions older than
`OPENZRO_FLOW_RETENTION`. There is **no** tiered movement of data
from hot to cold inside the management — that would require the cold
exporter to re-emit historical data, which is awkward and brittle.

Instead: hot and cold receive the **same** real-time write stream. If
both are configured, every event lands in both. Cold has everything
since archiving started; hot has only the configurable recent window.

The dashboard's "Network Traffic Events" page queries the hot store
exclusively and shows: "Older than {retention}? Query your archive
or SIEM."

### Backpressure

Ingest is a buffered fan-out: the gRPC handler enqueues events on
small per-destination channels and acks the peer immediately. If a
destination's channel fills, the handler logs a loud
`dropped flow event for destination=X: channel full` and increments
a counter. The peer ack is **not** delayed by destination latency —
peers should not hang because Datadog is slow.

Queue sizes are env-tunable:

```
OPENZRO_FLOW_BUFFER_HOT=10000      # in-memory before hot writer batches
OPENZRO_FLOW_BUFFER_STREAM=10000   # before SIEM exporter
OPENZRO_FLOW_BUFFER_ARCHIVE=50000  # before Parquet rotate
```

The `dropped flow event` log shape mirrors the existing `dropped
update for peer …: channel full` template referenced in the root
`CLAUDE.md`.

### What ships first (PR sequence)

1. ADR (this file)
2. **PR-A**: `flow.Store` interface + Postgres impl + daily retention job
3. **PR-B**: wire `FlowService.Events` from `08398f9e` to a
   `flow.Store` instance built from env
4. **PR-C**: HTTP API `/api/network-traffic-events` with the 8
   filters that NetBird exposes publicly (peer, IPs, ports, protocol,
   user, time range, type, rule_id)
5. **PR-D**: dashboard UI page mirroring the API
6. **PR-E**: extend activity exporter to flow events (SIEM stream)
7. **PR-F**: cold archive (S3 first, GCS later)
8. **PR-G**: ClickHouse store backend

PRs A–E are the MVP. F and G are the volume / retention story; they
can land out of order based on user demand.

## Consequences

### Positive

- **Self-hosted deployments get a working traffic-events feature out
  of the box** with Postgres only, matching NetBird Cloud's
  user-visible behavior except with configurable retention (NetBird
  Cloud is hard-coded at 7 days).
- **Operators with existing observability infra are first-class** —
  they can run with `engine=none` and rely entirely on their SIEM, or
  with cold archive for compliance retention.
- **The interface decouples the storage choice from the rest of the
  code**: a third backend (TimescaleDB, VictoriaLogs, …) is a single
  file; the gRPC handler, query API, and UI do not change.
- **Cold archive in Parquet keeps data portable** — operators are not
  locked into our schema; standard query tools work.

### Negative / risks

- **Operators must choose a backend**. The `engine=none` default is
  ergonomic for "I just want to try openZro" but produces an empty
  dashboard page; we mitigate with clear UI copy ("Configure
  `OPENZRO_FLOW_STORE_ENGINE` to enable") and a `make` target that
  spins up Postgres locally for dev.
- **Hot retention purge is a write-amplifying batch op on Postgres**.
  Monthly partitioning + `DROP PARTITION` makes this O(1) instead of
  O(rows), but assumes operators run a Postgres new enough to support
  declarative partitioning (PG 11+; we already require that elsewhere).
- **Cold archive is "fire and forget"** — if the bucket is misconfigured
  we log loud and drop. We do not attempt durability with local
  retry queues; the retry loop lives in the operator's monitoring.

### Neutral

- **No tiered migration of hot → cold inside the management.** Mature
  systems sometimes do this; we explicitly do not, because it
  complicates failure modes. Operators who want it run their own
  archival job against the hot DB.

## References

- [ADR-0001 §3.3](0001-openzro-foundation.md#33-clean-room-reimplementation-policy) — clean-room policy under which this ADR was researched
- [`flow/proto/flow.proto`](../../flow/proto/flow.proto) — the BSD-3 protocol definition
- [`management/server/activity/exporter/`](../../management/server/activity/exporter/) — the SIEM exporter pattern reused for flow events
- [`management/server/flow_service.go`](../../management/server/flow_service.go) — current ack-only handler that PR-B extends
- NetBird traffic-events public docs: <https://docs.netbird.io/manage/activity/traffic-events-logging>
- NetBird streaming export public docs: <https://docs.netbird.io/manage/activity/event-streaming>
- ClickHouse license + sizing: <https://clickhouse.com/docs/en/about-us/distinctive-features>
- Apache Parquet format: <https://parquet.apache.org/docs/file-format/>
