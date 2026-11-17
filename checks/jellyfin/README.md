# jellyfin

Files: [restorecheck.yaml](restorecheck.yaml) (the check), [seed.yaml](seed.yaml) (test data).

Apply the check: `kubectl apply -k checks/jellyfin -n <namespace>`.

## Status

PASS on kind, 2026-11-14/15: Kopiur round trip passed, and the negative test (broken canary)
failed as expected. Harness: `hack/check-test/roundtrip.sh jellyfin`.

## Data kind and volumes

- **Kind:** sqlite-in-volume (library.db, jellyfin.db) + media files
- **Volumes** (`sourcePVC` -> mount): `config` -> `/config`, `media` -> `/media`

## What it checks

`/health` Healthy, canary media file sha256.

## Canary

Canary contract: /media/bret-canary/canary.txt and canary.sha256 (sha256sum format). Seed the canary
before the backup.

## Secrets

None.

## UIDs and securityContext

- `config-perms` (busybox:1.37): image default
- `check` (jellyfin/jellyfin:10.9.11): uid 1000

## Caveats and open issues

- The media PVC can be very large. Option: restore the config only. The image has no sqlite3, so the
  DB integrity is not checked.
- `library.db` stores the media item paths and IDs. If the media comes from a different point, items
  show as missing or unavailable, and the metadata is wrong. Handling: restore config and media in
  one attempt. Mount media at its original path. Check: the canary media file hash and `/health`.
  Option: restore the config only if the media is too large. In that case, the check covers the DB
  only.
