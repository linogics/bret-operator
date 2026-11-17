# n8n

Files: [restorecheck.yaml](restorecheck.yaml) (the check), [seed.yaml](seed.yaml) (test data).

Apply the check: `kubectl apply -k checks/n8n -n <namespace>`.

## Status

PASS on kind, 2026-11-14/15: Kopiur round trip passed, and the negative test (broken canary)
failed as expected. Harness: `hack/check-test/roundtrip.sh n8n`.

## Data kind and volumes

- **Kind:** sqlite-in-volume (database.sqlite)
- **Volumes** (`sourcePVC` -> mount): `data` -> `/home/node/.n8n`

## What it checks

decrypted credential and workflow export contain the canary (proves `N8N_ENCRYPTION_KEY`), app
`/healthz` ok.

## Canary

Canary contract: workflow named bret-canary and credential named bret-canary.

## Secrets

`n8n-secrets`. Create them with the source values.

## UIDs and securityContext

- `data-perms` (busybox:1.37): image default
- `check` (n8nio/n8n:1.60.0): uid 1000

## Caveats and open issues

- The encryption key is in a Secret, not in the volume. Postgres installs are not covered.
- The DB stores credentials encrypted with `N8N_ENCRYPTION_KEY`. A DB restored with a different key
  cannot decrypt the credentials. Workflows break silently. Check: the decrypted export must contain
  the canary credential. A failure here means a key mismatch, not a bad backup.
