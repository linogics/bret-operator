---
title: Backup providers
description: Velero, VolSync and Kopiur compared, what each can and cannot do with bret, and how to pick one.
---

bret restores with a backup tool that you already run: Velero, VolSync or Kopiur. It never moves
backup data itself. It creates the restore object of the tool, waits for it, then runs your check.

```d2 title="bret drives the restore object of one of three backup tools, all reading from S3"
bret: bret Restore {
  top: 0
  left: 400
  width: 280
  height: 80
}
velero: "Velero\nRestore" {
  top: 140
  left: 0
  width: 280
  height: 100
}
volsync: "VolSync\nReplicationDestination" {
  top: 140
  left: 400
  width: 280
  height: 100
}
kopiur: "Kopiur\nRestore" {
  top: 140
  left: 800
  width: 280
  height: 100
}
s3: S3 bucket {
  shape: cylinder
  top: 140
  left: 1280
  width: 160
  height: 140
}
scratch: "scratch PVCs\n+ check pod" {
  top: 300
  left: 400
  width: 280
  height: 100
}
bret -> velero: creates
bret -> volsync: creates
bret -> kopiur: creates
s3 -> velero: backup data
s3 -> volsync: backup data
s3 -> kopiur: backup data
velero -> scratch: restores
volsync -> scratch: restores
kopiur -> scratch: restores
```

## Compare providers

| | Velero | VolSync | Kopiur |
|---|---|---|---|
| Maturity in bret | Supported | Supported | Supported |
| Helm switch | `providers.velero.enabled`, off by default | `providers.volsync.enabled`, off by default | `providers.kopiur.enabled`, on by default |
| Backup object bret reads | `Backup`, phase `Completed` | `ReplicationSource`, its `status.lastSyncTime` | `Snapshot`, phase `Succeeded` |
| One backup holds | Every PVC of the namespace | One PVC | One PVC |
| Restore object bret creates | `velero.io/v1` `Restore` | `ReplicationDestination` with a manual trigger | `kopiur.home-operations.com/v1alpha1` `Restore` |
| What gets restored | PVCs and PVs only, no other resources | One PVC per source | One PVC per source |
| Where the restored PVCs live | Scratch namespace `bret-scratch-<hash>` | The `Restore` namespace | The `Restore` namespace |
| All PVCs from one point in time | Yes, one `Backup` | No, one sync per PVC | No, one `Snapshot` per PVC |
| Restore an older backup by hand | `backupRef` names any `Backup` | `restoreAsOf` picks the newest restic snapshot at or before a time | `backupRef` names any `Snapshot` |
| Scratch size | Velero restores at the original size and storage class | `size` or `autoExpand` | `size` or `autoExpand` |
| Secrets for the check | Copied by bret into the scratch namespace | Read from the `Restore` namespace | Read from the `Restore` namespace |
| Block mode volumes | Not supported | Not supported | Not supported |
| Throughput limits | Concurrency only, see [throttling](/providers/shared-setup/#throttle-restore-throughput) | Mover CPU and memory only | Kopia bandwidth and ops cap per repository |
| Tested with | Velero 1.18.2 (chart 12.2.5), AWS plugin v1.14.4 | VolSync 0.17.0 | Kopiur 0.11.0 |

Block mode: the check pod mounts every restored volume as a file system, so a `volumeMode: Block`
PVC cannot be checked.

Each provider has its own section with an overview, a walkthrough and its limitations:
[Velero](/providers/velero/), [VolSync](/providers/volsync/), [Kopiur](/providers/kopiur/).
Setup that applies to all three (CSI snapshots, S3, point in time, throttling) is on
[Shared setup](/providers/shared-setup/).

## Which tool to choose

| Need | Pick |
|---|---|
| Whole-namespace backups, many resource types, one tool for the cluster | Velero |
| All PVCs of an app from one point in time, without quiescing the app | Velero |
| Per-volume backups with restic, scheduled by the volume | VolSync |
| Per-volume backups with Kopia, deduplication, and a repository you manage | Kopiur |

All three work with the same check. The check does not care which tool made the data.

A `Restore` or `RestoreTest` for a disabled provider stops with reason `ProviderUnsupported`.

## The walkthrough example

Every walkthrough uses the same WordPress + MariaDB app, which has two PVCs:

- `html`: the WordPress docroot, `wp-config.php` and `wp-content`. Owned by uid 33.
- `mysql`: the MariaDB data dir. Owned by uid 999.

The example files are in [examples/](https://github.com/linogics/bret-operator/tree/main/examples):
`examples/wordpress/` is the app and the check, `examples/<tool>/` adds the backup setup, a
`Restore`, a `RestoreTest`, and a negative `Restore` under `negative/`. The files were tested on a
kind cluster with `hack/check-test/operator-roundtrip.sh <tool>` (see
[Test a check by hand](/guides/test-a-check/)).
