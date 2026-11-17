# postgres

Files: [restorecheck.yaml](restorecheck.yaml) (the check), [seed.yaml](seed.yaml) (test data).

Apply the check: `kubectl apply -k checks/postgres -n <namespace>`.

## Status

PASS on kind, 2026-11-14/15: Kopiur round trip passed, and the negative test (broken canary)
failed as expected. Harness: `hack/check-test/roundtrip.sh postgres`.

## Data kind and volumes

- **Kind:** database-only
- **Volumes** (`sourcePVC` -> mount): `data` -> `/var/lib/postgresql/data`

## What it checks

start with crash recovery, `pg_amcheck --heapallindexed` on every DB, canary row sha256.

## Canary

Canary contract: table bret_canary(id int, payload text, sha256 text) in db "app".

## Secrets

None.

## UIDs and securityContext

- `pgdata-perms` (busybox:1.37): uid 999
- `check` (postgres:17.12): uid 999

## Caveats and open issues

- Canary table `bret_canary` must exist in db `app`. The backup must be crash-consistent. The PGDATA
  owner must be uid 999.
- The check reads the DB's own structure (`pg_amcheck`, `mysqlcheck`, `validate`) and the canary
  row. A physical copy taken while the DB runs is recovered by crash recovery. That recovery is part
  of the test.
