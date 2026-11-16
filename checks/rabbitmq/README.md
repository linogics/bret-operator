# rabbitmq

Files: [restorecheck.yaml](restorecheck.yaml) (the check), [seed.yaml](seed.yaml) (test data).

Apply the check: `kubectl apply -k checks/rabbitmq -n <namespace>`.

## Status

PASS on kind, 2026-11-14/15: Kopiur round trip passed, and the negative test (broken canary)
failed as expected. Harness: `hack/check-test/roundtrip.sh rabbitmq`.

## Data kind and volumes

- **Kind:** files-only (mnesia dir + message store)
- **Volumes** (`sourcePVC` -> mount): `data` -> `/var/lib/rabbitmq`

## What it checks

server boot, `await_startup`, `check_running`, `check_local_alarms`, canary queue message count.

## Canary

Canary contract: queue bret.canary holds BRET_CANARY_MESSAGES messages. Seed the canary before the
backup.

## Secrets

None.

## UIDs and securityContext

- `check` (rabbitmq:3.13-management): uid 999

## Caveats and open issues

- The node name is tied to mnesia (`RABBITMQ_NODENAME`). `.erlang.cookie` must survive the restore
  with mode 400.
- The test runs as `rabbit@localhost`. A real check must use the source node name.
- Each has its own on-disk format. The checks rely on the server to load the restored data and
  answer a query for the canary.
