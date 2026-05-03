# ADR-0012: Cold-archive read path for flow events (Parquet + DuckDB)

**Status:** Proposed — 2026-05-03
**Deciders:** openZro maintainers
**Supersedes / amended by:** —
**Tracking issue:** [openzro/openzro#10][issue-10]

## Context

[ADR-0002][adr-2] split flow event persistence into two tiers:

* a **hot store** (Postgres / MySQL / SQLite via GORM) that backs the
  dashboard's [`/api/network-traffic-events`][api] endpoint and is
  retention-bounded by `OPENZRO_FLOW_RETENTION` (default 720h),
* a fan-out of **sinks** (S3 / GCS object storage, Datadog, Elastic, HTTP
  webhook) that receive every event but are write-only.

That model maps cleanly to [`flow/store/store.go`][store-go]: `Store` is
the queryable Sink the management binary picks at boot, and `Sink` is
everything else. Per-account flow throughput at our typical scale ranges
from 10⁴ to 10⁸ events / day, so retention beyond a few weeks blows up
hot-store cost — operators are explicitly nudged toward an external
archive in the chart README.

The gap that surfaces in production: the dashboard's Network Traffic page
already tells the operator that

> Older events live in your configured streaming target (SIEM) or cold
> archive.

(see [`dashboard/src/app/(dashboard)/events/network-traffic/page.tsx`][page-tsx])

…but the UI cannot actually reach the archive. Once the hot retention
expires, the dashboard's date picker returns an empty list for any
`since` < `now - retention` even when the operator can clearly see the
matching `*.ndjson.gz` objects in the bucket. SIEM exporters
(Datadog / Elastic / HTTP) are intentionally fire-and-forget — the
security team queries those tools directly and that is the right
pattern. Cold archive is different: it is the operator's own data, and
losing dashboard reach makes it feel forgotten.

This ADR closes that gap.

## Decision

**Federate the dashboard's flow-events query across two read backends.**

```text
                 [/api/network-traffic-events]
                              │
                  (since/until vs hot retention?)
                              ├── all in retention  → hot Store (today)
                              └── partial / outside → federated Store
                                                       ├─ hot Store     (in-window slice)
                                                       └─ archive Store (Parquet via DuckDB)
```

Two concrete pieces:

1. **Write side — Apache Parquet** as the archive object format.
   `flow/sinks/s3.go` and `flow/sinks/gcs.go` learn a
   `OPENZRO_FLOW_ARCHIVE_FORMAT` env knob (`ndjson` | `parquet`) defaulting
   to **`parquet` for new installs** and **`ndjson` for existing operators
   who set neither** (back-compat — see Migration below). Same partition
   layout the sinks already write today (Hive-style for Athena / Glue
   / DuckDB-friendly):

   ```text
   <prefix>/year=YYYY/month=MM/day=DD/account=<id>/<unix-nano>-<rand>.parquet
   ```

   Parquet earns this slot over alternatives because of:
   * column-level compression — empirically 3-10× smaller than `*.ndjson.gz`
     for the flow event schema (high redundancy in `peer_id`, `protocol`,
     `direction`, `rule_id`),
   * per-row-group min/max statistics — DuckDB's predicate pushdown skips
     blocks that cannot match `WHERE received_at BETWEEN ...` or
     `WHERE peer_id = ...`,
   * a stable schema language — Protobuf → Parquet logical types are
     well-defined, evolution discipline can be enforced (additive-only,
     see §"Schema evolution"),
   * universal readability — Athena, BigQuery, Spark, Polars, DuckDB and
     pandas all read it natively, so the operator keeps an escape hatch
     to query the archive without ever touching the openZro binary.

2. **Read side — embedded DuckDB.** A new `flow/store/archive/` package
   implements the `Store` interface (Save + Query + Purge) by issuing
   DuckDB SQL against the bucket:

   ```sql
   SELECT * FROM read_parquet(
     's3://bucket/<prefix>/year=*/month=*/day=*/account=<id>/*.parquet',
     hive_partitioning = true
   )
   WHERE received_at BETWEEN $since AND $until
     AND peer_id = $peer_id  -- when set
   ORDER BY received_at DESC
   LIMIT $limit OFFSET $offset
   ```

   `hive_partitioning = true` lets DuckDB prune entire `year=/month=/day=`
   directories before opening any object — important for the long-window
   queries forensics ends up issuing.

   DuckDB lives **in-process**: it is a Go-importable library
   ([`github.com/marcboeker/go-duckdb`][go-duckdb]), not a separate
   service. No new container, no replicaset, no port to expose. The
   management binary grows by roughly 30–40 MB to ship the engine —
   acceptable cost given the binary already carries gorm + grpc + the
   Postgres / MySQL / SQLite drivers.

3. **Federation layer — `flow/store/federated/`.** A small wrapper that
   takes `{hot Store, archive Store}` and dispatches by query window:

   * window fully inside hot retention → hot only
   * window fully outside hot retention → archive only
   * window crosses the retention boundary → both, merged by
     `received_at` desc

   The wrapper also implements `Save` / `Purge` so the rest of the
   binary stays unaware of the split; writes still hit hot, purge still
   targets hot, and the archive sinks receive their events on the
   FlowService fan-out exactly as today.

## Decisions resolved here

### 1. Format default and migration

* **New installs** default to Parquet. Operators bringing up a fresh
  cluster after this ADR ships get the federated read for free.
* **Existing installs** keep NDJSON until they flip
  `OPENZRO_FLOW_ARCHIVE_FORMAT=parquet`. The flip is one-way for the
  archive (you can keep producing both formats during a transition by
  running two sinks, but the federated read recognises only Parquet).
* The federated read **does not** transparently bridge NDJSON → Parquet.
  A separate one-shot tool (a follow-up ADR — out of scope for this
  one) will re-emit historical NDJSON as Parquet when an operator wants
  contiguous history.

This back-compat keeps the upgrade path frictionless: deploy the new
management binary, observe that Parquet writes start happening for new
events, optionally run the tool against old prefixes once it ships.

### 2. CGo dependency

`go-duckdb` requires CGo. We accept that cost for the management binary
because:

* Linux + macOS CGo cross-compile is well-trodden via [goreleaser][gor]
  matrices that already exist in the repo (`.goreleaser*.yaml`).
* Windows CGo for management is the riskiest target. We **gate the
  archive store behind a build tag** `archive_duckdb`. Linux and macOS
  builds turn it on by default; the Windows binary ships without the
  archive read store until smoke tests on a Windows runner clear it.
  When that build tag is off, the federated layer detects the absent
  archive store and falls back silently to hot-only — same behaviour as
  today.
* No CGo on the dashboard, signal, relay, or client binaries. Only
  management.

### 3. Object-store auth in DuckDB

DuckDB exposes S3 + GCS via the [`httpfs`][httpfs] extension. We mirror
the credential model the sinks already use:

* **S3 / S3-compatible (MinIO, R2, …)**: read `AWS_ACCESS_KEY_ID`,
  `AWS_SECRET_ACCESS_KEY`, optional `AWS_SESSION_TOKEN` and
  `OPENZRO_FLOW_ARCHIVE_S3_ENDPOINT` from env. The archive store
  translates them into DuckDB `SET s3_*` settings on the per-query
  connection (DuckDB has [`SECRET` objects][duckdb-secret] for this
  pattern). Pod-level IAM (IRSA / Workload Identity) works because the
  underlying SDK reads the same env vars.
* **GCS**: read `GOOGLE_APPLICATION_CREDENTIALS` (path to the service
  account JSON) and pass through DuckDB's gcs extension when present,
  fall back to S3-compatible mode (`storage.googleapis.com` HMAC
  endpoint) otherwise. The chart's flow sink config already supports
  both; the archive store reuses the resolved values.

No new env vars introduced — the archive store reads what is already
configured.

### 4. Schema evolution discipline

Parquet's schema is generated from the `flow.proto` `FlowEvent` message
plus a small handful of management-side fields (`AccountID`, `PeerID`,
`ReceivedAt` — see [`flow/store/store.go::Event`][store-event]). To keep
historical Parquet files queryable indefinitely we adopt the same rule
ADR-0002 set for the hot tier:

* **Adding a column is fine** — Parquet readers tolerate missing columns
  and the federated layer fills them with the type's zero value.
* **Renaming a column is a breaking change** — must coincide with a
  major release and be paired with the re-emit tool.
* **Removing a column is fine** as long as the read code does not depend
  on it; otherwise treat as rename.

Enforce by code review; no automated check in v1 because the schema
surface is small and infrequent.

## Consequences

**Positive:**

* The dashboard's Network Traffic page reaches archive data without the
  operator leaving the UI. Investigations that span months stay in one
  flow.
* Smaller object storage bill — Parquet column compression beats
  `*.ndjson.gz` by 3-10× on flow event cardinality.
* Operators retain a vendor-neutral escape hatch — Parquet is readable
  by every analytical engine that matters.
* Federated layer makes the split invisible to callers; the rest of the
  binary still consumes a single `Store`.

**Negative:**

* Management binary grows ~30-40 MB.
* CGo dep adds a build constraint we do not have today (mitigated by
  `archive_duckdb` build tag for the unlikely-to-need-it cases).
* Operators on NDJSON archives need a one-shot re-emit if they want
  contiguous history — extra step at flip time.
* DuckDB's first query against a cold prefix takes longer (object
  listing + first row group fetch). We document this in the dashboard
  copy ("archive queries can take a few seconds") and consider a small
  per-account result cache if it becomes annoying.

**Neutral:**

* The Sink interface stays unchanged. Adding any future archive backend
  follows the same pattern: write-side emits Parquet under the same
  partition layout, read-side either pipes through DuckDB's existing
  fs adapter or implements `Store`.

## Alternatives considered

* **DuckDB reading NDJSON directly via `read_json_auto`.** Works, but
  loses every advantage of Parquet: no column pruning, no row-group
  skipping, full file scan on every query. Benchmarked at ~10× slower
  than Parquet for the typical filter shape (`peer_id` + window). Kept
  as the read path *only* for operators who explicitly stay on NDJSON,
  with a documented performance disclaimer.
* **ClickHouse with `S3` table engine.** More performant at PB scale,
  but adds a real service to run (replicaset + Keeper / ZooKeeper).
  Already framed in [ADR-0002][adr-2] as a future
  "medium-and-larger" engine path (the `clickhouse` slot in the
  engine matrix); not blocking this ADR.
* **AWS Athena / BigQuery as the read engine.** Vendor-locks the
  archive choice to one cloud, exposes the operator to per-query
  pricing they did not opt into, requires Glue catalog setup on the
  AWS side. Rejected.
* **Trino / Spark / DataFusion.** Equivalent technical class to DuckDB
  but each adds either a separate service (Trino, Spark) or a less
  ergonomic Go integration (DataFusion). DuckDB's Go binding is
  battle-tested and the SQL surface is closest to what the dashboard
  needs.

## Implementation sequence

1. ADR landed (this document).
2. Parquet writer for `flow/sinks/s3.go` (and a shared helper for
   format dispatch). Covers the new `OPENZRO_FLOW_ARCHIVE_FORMAT` knob.
3. Mirror for `flow/sinks/gcs.go`.
4. `flow/store/archive/` with DuckDB connection per query (no
   long-lived state — DuckDB attaches the bucket as a virtual filesystem
   on demand). Behind `archive_duckdb` build tag.
5. `flow/store/federated/` wrapping hot + archive, dispatch by date
   window. Replaces the direct hot-only wire-up in
   [`management/cmd/management.go`][mgmt-cmd].
6. Dashboard: update copy in [`page.tsx`][page-tsx] from
   "live in your configured cold archive" to a phrasing that reflects
   "queryable from this view"; the API URL doesn't change.
7. Smoke tests: hot-only window, archive-only window, mixed window;
   one S3 backend (MinIO), one GCS backend (fake-gcs-server).
8. ADR-0002 updated with a note pointing at this ADR and the resolved
   choice for the "future read path" placeholder it left open.

## References

* [ADR-0002 — flow events storage tiers and engine matrix][adr-2]
* [`flow/store/store.go`][store-go] — `Sink` and `Store` interfaces
* [`flow/sinks/s3.go`, `flow/sinks/gcs.go`][sinks] — current write-only
  archives
* [Issue #10][issue-10] — tracking issue for this ADR
* [DuckDB Parquet docs][duckdb-parquet]
* [DuckDB httpfs (S3 / GCS / R2)][httpfs]
* [`go-duckdb`][go-duckdb] — Go binding

[adr-2]: 0002-flow-events-storage.md
[api]: ../../management/server/http/handlers/network_events/network_events_handler.go
[page-tsx]: ../../dashboard/src/app/(dashboard)/events/network-traffic/page.tsx
[store-go]: ../../flow/store/store.go
[store-event]: ../../flow/store/store.go
[sinks]: ../../flow/sinks/
[mgmt-cmd]: ../../management/cmd/management.go
[issue-10]: https://github.com/openzro/openzro/issues/10
[go-duckdb]: https://github.com/marcboeker/go-duckdb
[duckdb-parquet]: https://duckdb.org/docs/data/parquet/overview
[httpfs]: https://duckdb.org/docs/extensions/httpfs/s3api
[duckdb-secret]: https://duckdb.org/docs/configuration/secrets_manager
[gor]: https://goreleaser.com
