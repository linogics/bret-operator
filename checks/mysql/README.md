# mysql

Files: [restorecheck.yaml](restorecheck.yaml) (the check), [seed.yaml](seed.yaml) (test data).

Apply the check: `kubectl apply -k checks/mysql -n <namespace>`.

## Status

PASS on kind, 2026-11-14/15: Kopiur round trip passed, and the negative test (broken canary)
failed as expected. Harness: `hack/check-test/roundtrip.sh mysql`.

## Data kind and volumes

- **Kind:** database-only
- **Volumes** (`sourcePVC` -> mount): `data` -> `/var/lib/mysql`

## What it checks

start mysqld on restored datadir, `mysqlcheck --check` on all DBs, canary row.

## Canary

Canary contract: table bret.canary(id int primary key, payload text, sha256 char(64)).

## Secrets

None.

## UIDs and securityContext

- `check` (mysql:8.4): uid 999

## Caveats and open issues

- UID 999 is assumed. performance_schema may print CHECK notes.
- The check reads the DB's own structure (`pg_amcheck`, `mysqlcheck`, `validate`) and the canary
  row. A physical copy taken while the DB runs is recovered by crash recovery. That recovery is part
  of the test.
