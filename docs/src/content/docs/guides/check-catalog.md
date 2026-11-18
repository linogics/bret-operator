---
title: Check catalog
description: Ready-made RestoreChecks for common apps and their test status.
---

One folder per app. Each holds the same files:

- `restorecheck.yaml`: the RestoreCheck. Apply it with `kubectl apply -k checks/<app> -n <namespace>`.
- `seed.yaml`: test data for `hack/check-test/roundtrip.sh <app>`. Not for production.
- `README.md`: status, volumes, what it checks, canary, Secrets, UIDs and caveats.

## Test status (kind, Kopiur round trip, 2026-11-14/15)

A PASS means two things. The round trip passed. The negative test (broken canary) failed, as expected.
Harness: `hack/check-test/roundtrip.sh APP`. Details: `docs/testing-checks.md`.

Every app below passed, except `minimal`, which is an example. Each README has the details.

Caveats that apply to every result:

- The restored mount roots come back owned by `root:65532`. Several checks change the owner of their mount in a root init container.
- Set pod-level settings, such as `securityContext.fsGroup`, in the check's pod fields. Most checks do not use `fsGroup`.
- Seeds contain test-only values. Their names start with `seed-...-not-secret`. A real check must use the source app's own Secrets.

Images were checked against public registries where noted. A `TAG UNVERIFIED` marker means the tag could not be confirmed at all.

## Conventions

- **restoredVolumes.** Every check lists its volumes as `{sourcePVC, mountPath}`. `sourcePVC` is the
  name of the PVC in the backed-up namespace. The Restore needs one source with the same `sourcePVC`
  for each entry. Single-PVC apps have one entry. Apps with two or more PVCs have one entry per PVC
  (nextcloud, wordpress, jellyfin, pihole). The table uses the seed PVC names. Rename `sourcePVC` to
  your PVC names.
- **Mount path = original mount path.** Restored volumes must mount at the path that the app used in
  production. For example: `/var/lib/postgresql/data` or `/data`. Absolute paths in the app config then
  still resolve. The example uses `/restore`. This is not required.
- **Canary contract.** Every data app must have a canary. The seeding job writes the canary before the
  backup. A canary is a row, file, doc or object. It holds a payload and its sha256. The check
  recomputes the hash on the restored data. This proves that the data is readable, not only present.
  The canary names are in each YAML header.
- **Native sidecars.** DB servers that an app needs at check time run as `initContainers` with
  `restartPolicy: Always` and a `startupProbe`. They keep running while the `containers` checks run.
  They require k8s 1.29+. A plain init container runs before the checks and exits; a native sidecar
  does not.
- **Secrets.** Checks read credentials from Secrets. Each YAML names its Secrets (`*-db`,
  `grafana-admin`, `n8n-secrets`). The operator or user must provision them with the source values.
  Encryption keys and admin passwords must match the source. If they do not match, the check fails for
  a reason that is unrelated to the backup.
- **Pod security.** The per-container `securityContext` sets the UID of each image: postgres 999,
  mysql 999, nextcloud and wordpress 33, grafana 472, prometheus 65534. Others use 1000 or the image
  default.
