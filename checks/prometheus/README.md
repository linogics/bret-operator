# prometheus

Files: [restorecheck.yaml](restorecheck.yaml) (the check), [seed.yaml](seed.yaml) (test data).

Apply the check: `kubectl apply -k checks/prometheus -n <namespace>`.

## Status

PASS on kind, 2026-11-14/15: Kopiur round trip passed, and the negative test (broken canary)
failed as expected. Harness: `hack/check-test/roundtrip.sh prometheus`.

## Data kind and volumes

- **Kind:** files-only (TSDB blocks + WAL)
- **Volumes** (`sourcePVC` -> mount): `data` -> `/prometheus`

## What it checks

`promtool tsdb list`, server ready, `max_over_time(bret_canary_seq[3650d])` equals seeded value.

## Canary

Canary contract: metric bret_canary_seq. Its last written value is BRET_CANARY_SEQ.

## Secrets

None.

## UIDs and securityContext

- `data-perms` (busybox:1.37): image default
- `check` (prom/prometheus:v2.54.1): uid 65534

## Caveats and open issues

- The canary metric must be ingested before the backup. Watch WAL replay time on large data.
- Each has its own on-disk format. The checks rely on the server to load the restored data and
  answer a query for the canary.
