---
title: Backup providers
description: Velero, VolSync and Kopiur compared, and how to pick one.
---

bret restores with a backup tool that you already run: Kopiur, VolSync or Velero. Each tool has
its own page. Every page walks through the same WordPress + MariaDB example, which has two PVCs:

- `html`: the WordPress docroot, `wp-config.php` and `wp-content`. Owned by uid 33.
- `mysql`: the MariaDB data dir. Owned by uid 999.

| Page | Backup object | One backup holds |
|---|---|---|
| [kopiur.md](/providers/kopiur/) | Kopiur `Snapshot` | one PVC |
| [volsync.md](/providers/volsync/) | VolSync `ReplicationSource` (its last sync) | one PVC |
| [velero.md](/providers/velero/) | Velero `Backup` | every PVC of the namespace |

Each page has the same steps:

1. Deploy WordPress + MariaDB.
2. Back up: a CSI snapshot of each PVC, then a copy to S3.
3. Write the check.
4. Restore by hand, then create a `RestoreTest`.
5. How bret picks backups.
6. Troubleshooting.

The example files are in [examples/](https://github.com/linogics/bret-operator/tree/main/examples): `examples/wordpress/` is the app and the
check, `examples/<tool>/` adds the backup setup, a `Restore`, a `RestoreTest`, and a negative
`Restore` under `negative/`. The files were tested on a kind cluster with
`hack/check-test/operator-roundtrip.sh <tool>` (see [testing-checks.md](/guides/test-a-check/)).


## Which tool to choose

| Need | Pick |
|---|---|
| Whole-namespace backups, many resource types, one tool for the cluster | Velero |
| Per-volume backups with restic, scheduled by the volume | VolSync |
| Per-volume backups with Kopia, deduplication, and a repository you manage | Kopiur |

All three work with the same check. The check does not care which tool made the data.

Enable a tool in the Helm chart with `providers.<name>.enabled`. Kopiur is on by default. Velero
and VolSync are off by default. A `Restore` or `RestoreTest` for a disabled tool stops with reason
`ProviderUnsupported`.
