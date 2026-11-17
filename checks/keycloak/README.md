# keycloak

Files: [restorecheck.yaml](restorecheck.yaml) (the check), [seed.yaml](seed.yaml) (test data).

Apply the check: `kubectl apply -k checks/keycloak -n <namespace>`.

## Status

PASS on kind, 2026-11-14/15: Kopiur round trip passed, and the negative test (broken canary)
failed as expected. Harness: `hack/check-test/roundtrip.sh keycloak`.

## Data kind and volumes

- **Kind:** database-only (external PostgreSQL)
- **Volumes** (`sourcePVC` -> mount): `data` -> `/var/lib/postgresql/data`

## What it checks

`kc.sh start` against restored DB (Liquibase), `/health/ready` UP, canary realm OIDC discovery.

## Canary

Canary contract: realm bret-canary exists in the restored DB. Secret keycloak-db holds
KC_DB_PASSWORD.

## Secrets

`keycloak-db`. Create them with the source values.

## UIDs and securityContext

- `pgdata-perms` (busybox:1.37): uid 999
- `postgres` (postgres:17.12): uid 999
- `check` (quay.io/keycloak/keycloak:26.0): image default

## Caveats and open issues

- The tag was pulled OK in the test. Start can be slow. There is no separate realm export.
- Postgres stops at pod end through the kubelet. It does not use kill -9.
- This check has no file store. If themes or providers live on a second PVC, they must be restored
  with the DB. Add a second restoredVolumes entry. Check: the realm OIDC discovery and
  `/health/ready`.
