# nextcloud

Files: [restorecheck.yaml](restorecheck.yaml) (the check), [seed.yaml](seed.yaml) (test data).

Apply the check: `kubectl apply -k checks/nextcloud -n <namespace>`.

## Status

PASS on kind, 2026-11-14/15: Kopiur round trip passed, and the negative test (broken canary)
failed as expected. Harness: `hack/check-test/roundtrip.sh nextcloud`.

## Data kind and volumes

- **Kind:** database + files
- **Volumes** (`sourcePVC` -> mount): `html` -> `/var/www/html`, `pgdata` ->
  `/var/lib/postgresql/data`

## What it checks

`occ status` (DB reachable), `occ integrity:check-core`, canary: `oc_filecache.size` equals the file
size on disk.

## Canary

See the summary above. The seed writes it before the backup.

## Secrets

`nextcloud-db`. Create them with the source values.

## UIDs and securityContext

- `html-perms` (busybox:1.37): image default
- `pgdata-perms` (busybox:1.37): uid 999
- `postgres` (postgres:17.12): uid 999
- `check` (nextcloud:29-apache): uid 33

## Caveats and open issues

- pdo_pgsql in the image is assumed. The canary path assumes one storage. `dbhost` is rewritten to
  localhost in the config.
- `/var/www/html/data` holds the files. The `oc_filecache` table in postgres holds the rows that
  list the files and their sizes. If the DB comes from 10:00 and the files come from 10:05, rows
  point at missing files, or files have no rows. Symptoms: broken file lists and "file not found" in
  the UI. `occ files:scan` can repair listings. It may drop or duplicate entries. Handling: restore
  both PVCs from one backup selector in one attempt. Check: the canary `oc_filecache.size` must
  equal the file size on disk.
