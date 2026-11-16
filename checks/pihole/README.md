# pihole

Files: [restorecheck.yaml](restorecheck.yaml) (the check), [seed.yaml](seed.yaml) (test data).

Apply the check: `kubectl apply -k checks/pihole -n <namespace>`.

## Status

PASS on kind, 2026-11-14/15: Kopiur round trip passed, and the negative test (broken canary)
failed as expected. Harness: `hack/check-test/roundtrip.sh pihole`.

## Data kind and volumes

- **Kind:** sqlite-in-volume (gravity.db) + config files
- **Volumes** (`sourcePVC` -> mount): `etc-pihole` -> `/etc/pihole`, `dnsmasq` -> `/etc/dnsmasq.d`

## What it checks

gravity.db integrity and canary adlist, canary DNS line in custom.list, `pihole-FTL dnsmasq-test`.

## Canary

Canary contract: the adlist https://bret-canary.invalid/list.txt is in gravity.db. The line
"10.0.0.53 bret-canary.test" is in /etc/pihole/custom.list. Seed both before the backup.

## Secrets

None.

## UIDs and securityContext

- `check` (pihole/pihole:2024.07.0): image default

## Caveats and open issues

- The `dnsmasq-test` subcommand is not confirmed in 2024.07. The FTL DB schema is tied to the image
  version.
- Adlists and custom DNS live in `gravity.db` and `custom.list`. The config files in
  `/etc/dnsmasq.d` must match the FTL version. A mix causes lists that are in the DB but not
  applied. It can also cause missing DNS records. Check: the canary adlist row in gravity.db, the
  canary line in custom.list, and that the dnsmasq config parses.
