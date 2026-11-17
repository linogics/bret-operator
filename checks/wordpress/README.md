# wordpress

Files: [restorecheck.yaml](restorecheck.yaml) (the check), [seed.yaml](seed.yaml) (test data).

Apply the check: `kubectl apply -k checks/wordpress -n <namespace>`.

## Status

PASS on kind, 2026-11-14/15: Kopiur round trip passed, and the negative test (broken canary)
failed as expected. Harness: `hack/check-test/roundtrip.sh wordpress`.

## Data kind and volumes

- **Kind:** database + files
- **Volumes** (`sourcePVC` -> mount): `html` -> `/var/www/html`, `mysql` -> `/var/lib/mysql`

## What it checks

mariadb ping, canary post content == sha256 of uploaded file.

## Canary

See the summary above. The seed writes it before the backup.

## Secrets

`wordpress-db`. Create them with the source values.

## UIDs and securityContext

- `html-perms` (busybox:1.37): image default
- `mysql-perms` (busybox:1.37): image default
- `mariadb` (mariadb:11.4): uid 999
- `check` (wordpress:6.6-php8.3-apache): uid 33

## Caveats and open issues

- The front-end render is not checked. Plugin-generated tables are not covered.
- `wp_posts` rows reference uploads in `wp-content/uploads`. A DB newer than the files causes broken
  images. A DB older than the files causes orphan uploads. Handling: same as nextcloud. Use one
  attempt and both volumes. Check: the canary post content must equal the sha256 of the uploaded
  canary file.
