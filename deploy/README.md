# Local deployment helpers

This directory holds compose files used by the top-level Makefile to
spin up infrastructure for development and HA smoke testing.

## `dev-deps.compose.yml`

The three services an openZro deployment may need:

| Service  | Port | Purpose                              |
|----------|-----:|--------------------------------------|
| Postgres | 5432 | management's persistent store        |
| Valkey   | 6379 | Redis-compatible broker (HA option)  |
| NATS     | 4222 | NATS broker with JetStream (HA option) |

Brought up by `make dev.deps.up`, torn down by `make dev.deps.down`.
Choose **one** of Valkey or NATS at runtime via the corresponding
`OPENZRO_*_URL` env var; both are running in compose for convenience.

## `ha-local.compose.yml` *(planned)*

A 2-management + 2-signal cluster on top of the dev dependencies, used
to validate the cross-instance peer fan-out wired in commit
`3e8d5079`. Not yet implemented — once the management Dockerfile is
adjusted for HA env vars, this compose file should:

  - bring up the dev deps (or assume `dev.deps.up` already ran)
  - start two `management` containers sharing the Postgres + broker
  - start two `signal` containers sharing the broker
  - put a basic load balancer (Caddy or nginx) in front for client traffic

Until that lands, `make ha.up` will fail with `file not found`.
