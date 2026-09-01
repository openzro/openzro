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

No pre-upgrade action is required for the route validation change.

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

## Known limitations

### Multi-replica: one known invariant is still enforced only in-process

A management deployment running **more than one replica** can still
produce duplicates for one known case, because the check is a read
followed by a write protected by a lock that lives inside one process:

- group names issued through the API

DNS zone overlap is **not** in that list. It is enforced on both
engines by validation under the account-row serialization point, and a
concurrent loser receives an actionable overlap error.

Single-replica deployments are unaffected by any of this — the
in-process lock is sufficient there. Progress is tracked in
[#143](https://github.com/openzro/openzro/issues/143).

## Earlier releases

No pre-upgrade checks were required before `v0.53.1-alpha.89`.
