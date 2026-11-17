# elasticsearch

Files: [restorecheck.yaml](restorecheck.yaml) (the check), [seed.yaml](seed.yaml) (test data).

Apply the check: `kubectl apply -k checks/elasticsearch -n <namespace>`.

## Status

PASS on kind, 2026-11-14/15: Kopiur round trip passed, and the negative test (broken canary)
failed as expected. Harness: `hack/check-test/roundtrip.sh elasticsearch`.

## Data kind and volumes

- **Kind:** files-only (Lucene in path.data)
- **Volumes** (`sourcePVC` -> mount): `data` -> `/usr/share/elasticsearch/data`

## What it checks

single-node start, cluster yellow, canary doc sha256.

## Canary

Canary contract: index bret-canary. Doc _id "canary" holds {"payload": "...", "sha256": "..."}. Seed
the canary before the backup. cluster.name and node.name must equal the source values.

## Secrets

None.

## UIDs and securityContext

- `check` (docker.elastic.co/elasticsearch/elasticsearch:8.15.0): uid 1000

## Caveats and open issues

- The tag was pulled OK in the test. `cluster.name` and `node.name` must match the source. The host
  needs `vm.max_map_count`. UID 1000.
- The heap is 512m in the test. This fits the test cluster.
- Each has its own on-disk format. The checks rely on the server to load the restored data and
  answer a query for the canary.
