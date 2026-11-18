---
title: VolSync limitations
description: What bret cannot do with VolSync, and fixes for known errors.
---

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

### Restore fails with `RepositoryMissing`

- **Cause:** the `ReplicationSource` has no `spec.restic.repository`, or its Secret is not in the
  `Restore` namespace.
- **Fix:** keep the repository Secret next to the `Restore`.
