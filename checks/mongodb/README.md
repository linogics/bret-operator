# mongodb

Files: [restorecheck.yaml](restorecheck.yaml) (the check), [seed.yaml](seed.yaml) (test data).

Apply the check: `kubectl apply -k checks/mongodb -n <namespace>`.

## Status

PASS on kind, 2026-11-14/15: Kopiur round trip passed, and the negative test (broken canary)
failed as expected. Harness: `hack/check-test/roundtrip.sh mongodb`.

## Data kind and volumes

- **Kind:** database-only (WiredTiger)
- **Volumes** (`sourcePVC` -> mount): `data` -> `/data/db`

## What it checks

`mongod --fork` on restored dbpath, `validate full` on canary collection, canary sha256.

## Canary

Canary contract: db "app", collection bret_canary. Doc: {_id: "canary", payload, sha256}.

## Secrets

None.

## UIDs and securityContext

- `check` (mongo:7.0): uid 999

## Caveats and open issues

- TAG OK (mongo:7.0 probed). `mongosh` presence in the image NOT confirmed. Replica-set DBs are not
  covered.
- The check reads the DB's own structure (`pg_amcheck`, `mysqlcheck`, `validate`) and the canary
  row. A physical copy taken while the DB runs is recovered by crash recovery. That recovery is part
  of the test.
