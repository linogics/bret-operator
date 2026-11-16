# gitea

Files: [restorecheck.yaml](restorecheck.yaml) (the check), [seed.yaml](seed.yaml) (test data).

Apply the check: `kubectl apply -k checks/gitea -n <namespace>`.

## Status

PASS on kind, 2026-11-14/15: Kopiur round trip passed, and the negative test (broken canary)
failed as expected. Harness: `hack/check-test/roundtrip.sh gitea`.

## Data kind and volumes

- **Kind:** sqlite-in-volume (gitea.db + git repos)
- **Volumes** (`sourcePVC` -> mount): `data` -> `/data`

## What it checks

`gitea web` boot, `/api/healthz` pass, canary repo raw file sha256.

## Canary

Canary contract: public repo bret/canary. It holds canary.txt and canary.sha256 (sha256sum format).

## Secrets

None.

## UIDs and securityContext

- `check` (gitea/gitea:1.22): uid 1000

## Caveats and open issues

- Only SQLite is covered. Postgres-backed installs need a separate DB restore, which is not covered.
  The repo and DB share one PVC, so one snapshot covers both.
- The DB and files share one PVC. One snapshot covers both. The risk is the SQLite file itself. The
  backup must be taken with the app stopped, or from a consistent snapshot. Checks: DB integrity,
  and the canary row or file.
