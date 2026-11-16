# uptime-kuma

Files: [restorecheck.yaml](restorecheck.yaml) (the check), [seed.yaml](seed.yaml) (test data).

Apply the check: `kubectl apply -k checks/uptime-kuma -n <namespace>`.

## Status

PASS on kind, 2026-11-14/15: Kopiur round trip passed, and the negative test (broken canary)
failed as expected. Harness: `hack/check-test/roundtrip.sh uptime-kuma`.

## Data kind and volumes

- **Kind:** sqlite-in-volume (kuma.db)
- **Volumes** (`sourcePVC` -> mount): `data` -> `/app/data`

## What it checks

read-only `integrity_check` and canary monitor row via the app's own sqlite3 module, server boot,
entry-page.

## Canary

Canary contract: a monitor named bret-canary exists in kuma.db. Seed it before the backup.

## Secrets

None.

## UIDs and securityContext

- `data-perms` (busybox:1.37): image default
- `check` (louislam/uptime-kuma:1.23.15): uid 1000

## Caveats and open issues

- The schema is for 1.x only. 2.x differs. UID 1000 is assumed. The entry-page grep is weak.
- The DB and files share one PVC. One snapshot covers both. The risk is the SQLite file itself. The
  backup must be taken with the app stopped, or from a consistent snapshot. Checks: DB integrity,
  and the canary row or file.
