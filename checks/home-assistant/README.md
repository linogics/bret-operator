# home-assistant

Files: [restorecheck.yaml](restorecheck.yaml) (the check), [seed.yaml](seed.yaml) (test data).

Apply the check: `kubectl apply -k checks/home-assistant -n <namespace>`.

## Status

PASS on kind, 2026-11-14/15: Kopiur round trip passed, and the negative test (broken canary)
failed as expected. Harness: `hack/check-test/roundtrip.sh home-assistant`.

## Data kind and volumes

- **Kind:** sqlite-in-volume (home-assistant_v2.db) + .storage files
- **Volumes** (`sourcePVC` -> mount): `data` -> `/config`

## What it checks

recorder DB integrity (read-only), canary file sha256, HTTP 401 from `/api/`.

## Canary

Canary contract: /config/bret-canary/canary.txt and canary.sha256 (sha256sum format). Seed the
canary before the backup.

## Secrets

None.

## UIDs and securityContext

- `config-perms` (busybox:1.37): image default
- `check` (homeassistant/home-assistant:2024.9.0): uid 1000

## Caveats and open issues

- Start can exceed the time budget on slow nodes. The recorder DB can be large.
- The DB and files share one PVC. One snapshot covers both. The risk is the SQLite file itself. The
  backup must be taken with the app stopped, or from a consistent snapshot. Checks: DB integrity,
  and the canary row or file.
