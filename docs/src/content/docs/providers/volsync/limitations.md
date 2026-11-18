---
title: VolSync limitations
description: What bret cannot do with VolSync, and fixes for known errors.
---

## Limits

- **Restic only.** bret reads `spec.restic` of the `ReplicationSource`. rclone, rsync and
  syncthing movers are not supported.
- **No point in time across PVCs.** Each PVC syncs on its own. The restored PVCs of one run can
  be minutes apart. See [Point in time across PVCs](/providers/shared-setup/#point-in-time-across-pvcs).
- **The newest sync is the backup.** VolSync keeps no object per backup. To test an older restic
  snapshot, create a `Restore` by hand with `restoreAsOf`.
- **Repository Secret next to the Restore.** The Secret must be in the `Restore` namespace.
- **One mover UID per volume.** All restored files belong to that UID.
- **Labels must match one ReplicationSource.** A `backupSelector` that matches more blocks the
  test.

## Troubleshooting

### VolSync mover: `permission denied` on the data directory

- **Cause:** the mover runs as the default non-database UID.
- **Fix:** set `moverSecurityContext` in the `ReplicationSource` to the owner UID, and the
  `bret.linogics.io/mover-uid` annotation on the check for the restore.

### VolSync: `permission denied` after changing the mover UID

- **Cause:** the cache PVC was created by the old mover UID.
- **Fix:** delete the cache PVC. VolSync creates a new one.

### RestoreTest stops at `BackupSelectorAmbiguous`

- **Cause:** the `labels` of a source match more than one `ReplicationSource`.
- **Fix:** use `backupSelector.name`, or labels that only one `ReplicationSource` carries.
  Reason: [Failure reasons](/reference/failure-reasons/).

### Restore fails with `RepositoryMissing`

- **Cause:** the `ReplicationSource` has no `spec.restic.repository`, or its Secret is not in the
  `Restore` namespace.
- **Fix:** keep the repository Secret next to the `Restore`.
  Reason: [Failure reasons](/reference/failure-reasons/).

### Restore fails with `SourceMismatch`

- **Cause:** `spec.sourcePVC` of the `ReplicationSource` is not the `sourcePVC` of the source.
- **Fix:** point `backupRef` or `backupSelector` at the `ReplicationSource` of that PVC.
  Reason: [Failure reasons](/reference/failure-reasons/).

### Restore fails with `BackupNotFound` and `restoreAsOf`

- **Cause:** restic has no snapshot at or before `restoreAsOf`. The mover exits 0 but restores
  nothing, and bret fails the attempt instead of checking an empty volume.
- **Fix:** pick a later `restoreAsOf`, or check `restic snapshots` in the repository.
  Reason: [Failure reasons](/reference/failure-reasons/).
