# minimal

Files: [restorecheck.yaml](restorecheck.yaml) (the check), [seed.yaml](seed.yaml) (test data).

Apply the check: `kubectl apply -k checks/minimal -n <namespace>`.

## Status

Example check, not part of the catalog round trip. Harness:
`hack/check-test/roundtrip.sh minimal`.

## Data kind and volumes

- **Kind:** files-only (one canary file)
- **Volumes** (`sourcePVC` -> mount): `data` -> `/data`

## What it checks

The SHA-256 of the canary file matches the stored hash.

## Canary

checks/wordpress/restorecheck.yaml. The volume is restored to /data. The check recomputes the
SHA-256 of the canary file. Canary contract: /data/canary.txt holds the payload. /data/canary.sha256
holds its hash.

## Secrets

None.

## UIDs and securityContext

- `check` (busybox:1.37): uid 1000

## Caveats and open issues

None known.
