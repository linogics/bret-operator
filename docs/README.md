# bret-operator documentation

bret-operator tests your backups. It restores a backup into a scratch volume. It runs a check
against the restored data in a throwaway pod. Then it records the result on the custom resource,
as Kubernetes Events and as Prometheus metrics. The operator does not move backup data. It
drives the restore object of Velero, VolSync or Kopiur. Then it runs your check.

## Current status

Read this first.

| Part | Status |
|---|---|
| CRDs `Restore`, `RestoreTest`, `RestoreCheck` (schema, validation, printer columns) | Implemented. Generated from `api/v1alpha1/`. |
| Helm chart and plain manifests | Implemented. See [installation.md](installation.md). |
| Restore controller (Kopiur provider) | Implemented: scratch PVC, Kopiur restore, check pod, window, cleanup, retention, status, Events, metrics. Verified on a kind cluster (success and failure). |
| Velero and VolSync providers | Implemented: Velero `Restore` and VolSync `ReplicationDestination` (restic mover). Each provider is enabled in the Helm chart with `providers.<name>.enabled`. A disabled provider reports `ProviderUnsupported`. |
| Scratch volume size | Implemented: `target.size`, or the size derived from the source, with `target.autoExpand`. Reasons `SizeUnknown` and `InsufficientCapacity`. |
| `restoredVolumes` | Implemented for one entry per check (`name`, `mountPath`, `sourcePVC`). More than one entry fails with `ProviderUnsupported` in v1. |
| RestoreTest controller | Implemented: cron, afterBackup, manual trigger, overlap policy, retries. Unit tests only. Not yet run end to end on a cluster. |
| `watchNamespaces` (chart) | Implemented: limits the controller cache and namespaced Roles to a list of namespaces. |
| Metrics `bret_restore_*`, `bret_restoretest_*` | Implemented. Not yet checked by a Prometheus scrape. |
| `RestoreCheck` check files in `checks/` | Drafts. Being tested by hand with the harness in [testing-checks.md](testing-checks.md). Each app's result is in [checks/INDEX.md](../checks/INDEX.md). |
| Check check by hand (`hack/check-test/`) | Works. Restores a backup with Kopiur and runs a check against it. |

Not in v1: `retainedFor` (a TTL for kept resources). Remove kept resources by hand. Anything
still marked **planned** in these docs is not in the code yet. The design is in
[superpowers/specs/2026-11-14-bret-operator-restore-test-design.md](superpowers/specs/2026-11-14-bret-operator-restore-test-design.md).

## Read in this order

1. [concepts.md](concepts.md): what the operator does, the restore test lifecycle, the rules.
2. [getting-started.md](getting-started.md): tutorial from an empty cluster to a first check.
3. [installation.md](installation.md): install and upgrade the operator (Helm or manifests).
4. [backup-providers.md](backup-providers.md): set up Velero, VolSync or Kopiur so restores work.
5. [checks.md](checks.md): write a `RestoreCheck` check.
6. [restoretest-guide.md](restoretest-guide.md): schedule and tune restore tests.
7. [testing-checks.md](testing-checks.md): run a check by hand before the operator does it.

## Reference

| Document | Content |
|---|---|
| [reference/api.md](reference/api.md) | Generated field reference for all three kinds. Do not edit by hand. |
| [checks/INDEX.md](../checks/INDEX.md) | Catalog of app checks, their volumes, checks and test status. |
| [best-practices.md](best-practices.md) | Rules for backups and restore tests. |
| [data-and-database.md](data-and-database.md) | Apps that have files and a database. Keep them in one restore point. |
| [troubleshooting.md](troubleshooting.md) | Known errors and their fixes, from the kind spike. |
| [spike-findings.md](spike-findings.md) | Verified facts, versions and pitfalls from the kind spike. |

## Contributing

| Document | Content |
|---|---|
| [development.md](development.md) | Build, test, lint, generate docs, and the local kind setup. |
| [releasing.md](releasing.md) | How to cut a release and what it publishes. |
