# grafana

Files: [restorecheck.yaml](restorecheck.yaml) (the check), [seed.yaml](seed.yaml) (test data).

Apply the check: `kubectl apply -k checks/grafana -n <namespace>`.

## Status

PASS on kind, 2026-11-14/15: Kopiur round trip passed, and the negative test (broken canary)
failed as expected. Harness: `hack/check-test/roundtrip.sh grafana`.

## Data kind and volumes

- **Kind:** sqlite-in-volume (grafana.db)
- **Volumes** (`sourcePVC` -> mount): `data` -> `/var/lib/grafana`

## What it checks

server boot, `/api/health` database ok, canary dashboard via admin API.

## Canary

Canary contract: dashboard with uid and title "bret-canary". Seed it before the backup.

## Secrets

`grafana-admin`. Create them with the source values.

## UIDs and securityContext

- `data-perms` (busybox:1.37): image default
- `check` (grafana/grafana:11.2.0): uid 472

## Caveats and open issues

- The admin password must match the source. Provisioning files from the ConfigMap are not included.
- The DB and files share one PVC. One snapshot covers both. The risk is the SQLite file itself. The
  backup must be taken with the app stopped, or from a consistent snapshot. Checks: DB integrity,
  and the canary row or file.
