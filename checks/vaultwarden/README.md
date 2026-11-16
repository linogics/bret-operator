# vaultwarden

Files: [restorecheck.yaml](restorecheck.yaml) (the check), [seed.yaml](seed.yaml) (test data).

Apply the check: `kubectl apply -k checks/vaultwarden -n <namespace>`.

## Status

PASS on kind, 2026-11-14/15: Kopiur round trip passed, and the negative test (broken canary)
failed as expected. Harness: `hack/check-test/roundtrip.sh vaultwarden`.

## Data kind and volumes

- **Kind:** sqlite-in-volume (db.sqlite3, attachments, icons)
- **Volumes** (`sourcePVC` -> mount): `data` -> `/data`

## What it checks

python `integrity_check` (read-only), canary user row, app boots and `/alive` 200.

## Canary

Canary contract: user bret-canary@example.invalid exists in the users table. Seed the canary user
before the backup.

## Secrets

None.

## UIDs and securityContext

- `data-perms` (busybox:1.37): image default
- `sqlite-integrity` (python:3.12-slim-bookworm): image default
- `app-start` (vaultwarden/server:1.32.0): image default

## Caveats and open issues

- The tag was pulled OK in the test. Attachments are large. Migrations run on a restored DB.
- The DB and files share one PVC. One snapshot covers both. The risk is the SQLite file itself. The
  backup must be taken with the app stopped, or from a consistent snapshot. Checks: DB integrity,
  and the canary row or file.
