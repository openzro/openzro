# Upgrade notes

Read this before upgrading a management deployment. It lists, per
release, the checks to run **before** starting the new binary, and
separately the behavior changes to expect **after** it is running.

The two are different kinds of information and are kept apart on
purpose. A pre-upgrade check is something you act on now, at a
maintenance window. A behavior change is something support needs to
recognize next week.

## How migrations fail, and why that is intended

Some releases add database constraints that make an existing invariant
enforceable. Those migrations **refuse to run against data that already
violates the invariant**, and the management service does not start
until the data is corrected.

That is deliberate. The alternative is for the migration to pick which
row survives, which would hide from you that the violation happened at
all and would make a choice only you have the context to make.

What a failing migration guarantees is narrower than "nothing ran", and
worth stating precisely: it does not pick a winner, and it does not
correct, truncate or delete your data to make itself pass. The
management service will not start until the inconsistency is resolved.

It is not a single transaction, though. Schema changes that ran before
the failing one may already be applied, so treat a failed upgrade as a
database whose schema is part-way between the two versions — which is
another reason to take the backup below before starting.

## Unreleased

### Before upgrading

No pre-upgrade action is required for the route validation and group
serialization changes.

If you archive flow events to S3 or GCS in Parquet, see **Flow archive**
below before upgrading. Nothing breaks if you skip this, but the check is
easier to interpret before the reader's behavior changes.

### After upgrading

- **MySQL management transactions now run at READ COMMITTED.** This
  aligns MySQL with PostgreSQL for the store transactions opened by the
  management service. Concurrent conflicts that previously relied on
  InnoDB gap locks can now reach the explicit validation or unique
  constraint that explains them. For the known cases fixed so far, the
  loser gets an actionable conflict or validation error instead of a
  deadlock/internal error.
- **Editing a route can now expose duplicate direct-peer routes created
  by older versions.** Older releases could allow two routes in the
  same account with the same prefix or domain and the same direct peer.
  The new validation rejects that state. If editing either duplicate
  now returns "already has this route", delete one of the duplicate
  routes first; simply disabling or editing one duplicate can still be
  rejected because the other duplicate remains.
- **JWT group sync no longer joins a user to a manual/API group just
  because the IdP sends the same display name.** That unsafe join would
  let a manually created group grant access by name collision alone. If
  the IdP sends a name already used by an API group, openZro now creates
  or uses a separate JWT-managed group with the same name. Existing
  policies still point to group IDs, so policies that reference the API
  group will not automatically grant access to the new JWT group. Add
  the JWT group to the intended policies after it appears; ship the
  dashboard group-origin labels with this change so operators can tell
  the two groups apart.
- **Group display names are serialized per account and issuer/source.**
  Creating or renaming a group to a name already used by another group
  from the same source now returns a conflict across multi-replica
  management deployments instead of relying on process-local locking or
  database deadlocks. The source boundary is intentional: API, JWT and
  SCIM groups may still share the same display name, and IdP-managed
  groups never attach to API groups by name alone.
- **Linux management releases can read Parquet flow archives from the
  dashboard.** To make network traffic events older than
  `OPENZRO_FLOW_RETENTION` visible in the UI, the archive that writes
  the bucket must have effective format `parquet`. For env-configured
  archives, set `OPENZRO_FLOW_ARCHIVE_FORMAT=parquet` on the same
  deployment that writes `OPENZRO_FLOW_ARCHIVE_S3_BUCKET` or
  `OPENZRO_FLOW_ARCHIVE_GCS_BUCKET`. For dashboard-configured exports,
  set that export's Format to `parquet`; a row-level format overrides
  the env default, and the dashboard reader uses the same precedence.
  GCS archive reads also require DuckDB-compatible HMAC interoperability
  credentials: set `OPENZRO_FLOW_ARCHIVE_GCS_HMAC_KEY_ID` and
  `OPENZRO_FLOW_ARCHIVE_GCS_HMAC_SECRET` on management. The existing
  service account JSON/file settings can continue writing the archive,
  but DuckDB cannot read GCS with them; create the HMAC keys in Google
  Cloud Console under Cloud Storage -> Interoperability. A GCS Parquet
  reader configured without those two variables is disabled at
  management startup with a warning naming the missing variables; the
  dashboard keeps serving hot-store flow events, and archived GCS
  history becomes readable after the variables are set and management
  is restarted.
  The reader is assembled only when management starts. Saving or
  editing a dashboard export starts writing with the new format
  immediately, but archive reads from that export require a management
  restart. If more than one enabled S3/GCS export resolves to Parquet,
  the dashboard reader uses the first row by ID and logs the selected
  export; the other Parquet exports keep writing normally but are not
  queried by the UI.
  Archives written with the default NDJSON format remain available in
  the bucket for external tools, but the dashboard does not query them.

### Flow archive (S3 / GCS, Parquet)

Three changes ship together. The first two are improvements with no
action required; the third describes a gap you may need to decide about.

**Archive queries now skip files instead of reading every one.** The
reader filtered only on `received_at`, a column inside the Parquet files,
so any question opened every object for the account. Wide windows could
exceed the ingress timeout and return 504. Queries now also constrain the
`year`/`month`/`day` partition columns. No action required.

**`type` and `direction` filters now apply to archived events.** They
were accepted by the API and applied by the hot store, but ignored by the
archive reader — so a filtered query returned correctly filtered results
inside the retention window and unfiltered ones outside it. Expect a
filtered query spanning the archive to return **fewer** rows than before
the upgrade. That is the filter starting to work, not data loss.

**Objects are now written one per account and UTC date.** Both sinks
buffer into a queue shared by every account and flush on a timer, and
they used to name the whole object after the first event in the batch. A
batch that mixed accounts was filed entirely under one of them, and a
batch crossing midnight was filed under the earlier day.

Objects written before this release can therefore disagree with their own
path. The reader now compares `account_id` as well, so those rows are no
longer served to the wrong account. It cannot recover them for the right
one: the path is what selects which objects are opened, and a misfiled
object is still under the wrong prefix.

| | before upgrade | after upgrade |
| --- | --- | --- |
| wrong account sees the rows | yes | no |
| right account sees the rows | no | no |

**To find out whether you are affected**, run these against your archive
with the DuckDB CLI. Substitute your bucket, prefix and scheme (`s3://`
or `gcs://`), and configure credentials as your DuckDB install expects.

```sql
-- Events filed under the wrong account.
SELECT account AS path_account, account_id AS row_account, count(*)
FROM read_parquet('s3://BUCKET/PREFIX/year=*/month=*/day=*/account=*/*.parquet',
                  hive_partitioning=true)
WHERE account_id <> account
GROUP BY 1, 2
ORDER BY 3 DESC;
```

```sql
-- Events filed under the wrong day.
SELECT count(*)
FROM read_parquet('s3://BUCKET/PREFIX/year=*/month=*/day=*/account=*/*.parquet',
                  hive_partitioning=true)
WHERE date_trunc('day', received_at)
      <> make_date(year::INT, month::INT, day::INT);
```

Both read the whole archive, so run them off-hours on a large bucket.

**If either returns rows**, decide by what the cold archive is for:

- **Dashboard history only.** No action. The exposure is closed, queries
  are faster, and the affected events are older than your hot retention.
- **Audit or compliance.** The gap matters: those events are unreachable
  from the account that owns them. Repartitioning — rewriting the
  affected objects grouped by account and date — is the only fix, and it
  is not automated. Open an issue if you need it.

**If you raised the sink's flush interval** above the 15m default via
`OPENZRO_FLOW_ARCHIVE_{S3,GCS}_FLUSH_INTERVAL` or in the dashboard, keep
it set. The reader now reads that value to size how far its partition
filter reaches back, because a long flush is exactly what produced
objects whose contents predate their own name. Lowering or unsetting it
after the fact can hide older events written under the previous setting.

**Archive compaction is available as an explicit operator action.** Use
it only if the detection queries above return rows and the archive is
used for audit or compliance, not just dashboard history. It is not a
background task and management never runs it automatically.

Build management with DuckDB support and run:

```sh
openzro-mgmt flow-archive compact \
  --from 2026-06-01 \
  --to 2026-06-30 \
  --manifest /path/to/flow-archive-compact-2026-06.jsonl
```

The command is a dry run unless `--delete-originals` is present. It
requires `--from`, `--to` and `--manifest`, writes a JSONL manifest to a
new file, and refuses to overwrite an existing manifest. Dates are UTC
and inclusive. Today and yesterday are skipped because a sink may still
be writing those partitions. In dry-run entries, the manifest reports
planned replacement size as `bytes_planned`; `bytes_written` remains
zero because nothing was written to the bucket.

The command reads archive bucket settings from the same
`OPENZRO_FLOW_ARCHIVE_*` environment variables as management; non-secret
values can be overridden with flags. Secrets stay in environment
variables or credential files rather than command-line arguments, so they
do not land in shell history. For GCS, the DuckDB read side still needs
`OPENZRO_FLOW_ARCHIVE_GCS_HMAC_KEY_ID` and
`OPENZRO_FLOW_ARCHIVE_GCS_HMAC_SECRET`, while writes/deletes use the
service-account credential path already used by the GCS sink.

Keep `--concurrency` low unless the compaction pod has memory reserved
for it. Each worker opens its own DuckDB handle, and
`OPENZRO_FLOW_ARCHIVE_MEMORY_LIMIT` is per handle, not a global cap; the
default `--concurrency 1` is deliberately conservative. Verification
also has to re-read the replacement objects from the bucket. Today that
read is scoped by the compaction run ID rather than a narrow day range,
so very large archives pay extra object listing cost during backfills.
That cost is accepted to keep the delete gate simple and auditable.

To actually replace objects after reviewing the dry-run manifest, repeat
the same command with `--delete-originals`. The compactor writes
replacement objects, re-reads what landed in the bucket, compares a row
fingerprint, and only then deletes the original keys it listed at the
start of that day. If deletion fails before any original was removed, the
replacement is cleaned up. If deletion fails after at least one original
was removed, the replacement is kept; this can temporarily duplicate cold
results for that day, but it avoids losing the only good copy.


## v0.53.1-alpha.89

### Before upgrading

This release enforces in the database several invariants that were
previously held only in application code, and narrows two columns.

**Run every check below.** They cover the blocks known at the time of
this release: three for duplicate values, two for values too long for a
narrowed column. An empty result from all of them means these migrations
have nothing to refuse.

PostgreSQL:

```sql
-- 1. more than one account primary for the same private domain
SELECT LOWER(domain) AS domain, COUNT(*) AS accounts, STRING_AGG(id, ', ') AS account_ids
FROM accounts
WHERE is_domain_primary_account AND domain_category = 'private'
GROUP BY LOWER(domain) HAVING COUNT(*) > 1;

-- 2. duplicate network resource names within one account
SELECT account_id, name, COUNT(*) AS n, STRING_AGG(id, ', ') AS ids
FROM network_resources
GROUP BY account_id, name HAVING COUNT(*) > 1;

-- 3. duplicate posture check names within one account
SELECT account_id, name, COUNT(*) AS n, STRING_AGG(id, ', ') AS ids
FROM posture_checks
GROUP BY account_id, name HAVING COUNT(*) > 1;
```

MySQL and SQLite are the same queries with `GROUP_CONCAT(id)` in place
of `STRING_AGG(id, ', ')`, and `is_domain_primary_account = 1` in the
first.

Two more, because the same release also bounds those name columns at
128 characters. Duplicates are not the only thing that can stop the
upgrade, and the duplicate queries above would not show a name that is
merely too long.

```sql
-- 4. network resource names too long for the new column
SELECT id, account_id, CHAR_LENGTH(name) AS len
FROM network_resources
WHERE CHAR_LENGTH(name) > 128;

-- 5. posture check names too long for the new column
SELECT id, account_id, CHAR_LENGTH(name) AS len
FROM posture_checks
WHERE CHAR_LENGTH(name) > 128;
```

`CHAR_LENGTH` on PostgreSQL and MySQL; on SQLite use `LENGTH`. Counting
characters rather than bytes matters here — the bound is 128 characters,
so a name with accented or non-Latin text is not over the limit just
because it takes more bytes.

Rename anything these return before upgrading.

The two engines behaved differently here, and the difference was the
reason this release was held:

| | schema change | your data |
| --- | --- | --- |
| MySQL | fails, `Error 1406: Data too long` | preserved |
| PostgreSQL | **succeeded** | **silently truncated to 128 characters** |

PostgreSQL truncates because the change is applied as an explicit cast
to `varchar(128)`, and an explicit cast truncates where an assignment
would have raised an error. The upgrade completed, the service started,
and nothing said a name had been shortened.

That is fixed: the migration now refuses to run when a name is too
long, on every engine, and tells you to run the queries above. The
check is no longer something you have to remember — but run it in your
maintenance window anyway, so you find out at a moment of your choosing
rather than when the service declines to start.

### If any query returns rows

Do not upgrade yet. In order:

1. **Back up the database.** Everything below edits rows by hand.
2. **Audit the impact before changing anything.** The rows are not
   interchangeable, and which one to keep depends on how the deployment
   is used — see the notes below.
3. **Correct the data** so the invariant holds.
4. **Run the query again** until it returns nothing.

What to weigh in each case:

- **Duplicate primary accounts for a private domain.** Which account
  stays primary decides where users of that domain land when they log
  in. Both accounts may already have users, peers and policies. Look at
  which one is actually in use before clearing the flag on the other;
  this is a change in login behavior for real people, not a cleanup.
- **Duplicate network resource names.** Renaming is usually less
  disruptive than deleting, because a resource may be referenced by
  policies. Check what references each one first.
- **Duplicate posture check names.** Same reasoning: a check may be
  attached to policies. Confirm which duplicate is referenced before
  removing anything.

If the duplicates look like they were created by the races these
migrations close — two administrators acting at the same moment, or two
first logins from one company — the newer row is often the accidental
one. Often, not always. Confirm rather than assume.

### After upgrading

- **Renaming a posture check to a name already used in the same account
  is now rejected.** The name was documented as unique per account but
  was only enforced when creating one. Update is now enforced too, so an
  API call that previously succeeded can now return a conflict. This is
  intended.
- **Network resource and posture check names are bounded at 128
  characters**, declared in the OpenAPI schema for both, on the write
  payloads as well as the responses. A create or update carrying a
  longer name is rejected.
- **Two people signing in from the same company at the same moment no
  longer creates two accounts for that domain.** The second login joins
  the first's account instead of creating a competing one.
- **Creating two DNS zones with overlapping domains at the same moment
  now returns a clear rejection on PostgreSQL** instead of an internal
  error. On MySQL this case still returns an internal error — see the
  limitation below.

## Earlier releases

No pre-upgrade checks were required before `v0.53.1-alpha.89`.
