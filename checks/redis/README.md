# redis

Files: [restorecheck.yaml](restorecheck.yaml) (the check), [seed.yaml](seed.yaml) (test data).

Apply the check: `kubectl apply -k checks/redis -n <namespace>`.

## Status

PASS on kind, 2026-11-14/15: Kopiur round trip passed, and the negative test (broken canary)
failed as expected. Harness: `hack/check-test/roundtrip.sh redis`.

## Data kind and volumes

- **Kind:** database-only (RDB or AOF in /data)
- **Volumes** (`sourcePVC` -> mount): `data` -> `/data`

## What it checks

server load (refuses corrupt RDB/AOF), canary key sha256.

## Canary

Canary contract: keys bret:canary:payload and bret:canary:sha256 (hex sha256 of payload). Seed the
canary keys before the backup.

## Secrets

None.

## UIDs and securityContext

- `check` (redis:7.4): uid 999

## Caveats and open issues

- The AOF multipart format needs the same redis major version. Write the canary keys before the
  backup.
- The check reads the DB's own structure (`pg_amcheck`, `mysqlcheck`, `validate`) and the canary
  row. A physical copy taken while the DB runs is recovered by crash recovery. That recovery is part
  of the test.
