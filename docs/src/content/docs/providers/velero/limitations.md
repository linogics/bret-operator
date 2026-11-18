---
title: Velero limitations
description: What bret cannot do with Velero, and fixes for known errors.
---

## Troubleshooting

### Velero backup is gone after the source cluster is lost

- **Symptom:** the restore works only while the snapshot still exists in the cluster.
- **Cause:** the backup was created without data movement.
- **Fix:** create backups with `--snapshot-move-data` (or `snapshotMoveData: true` on the
  `Schedule`).

### Restore warning `ConfigMap:kube-root-ca.crt already exists`

- **Cause:** every namespace already has this ConfigMap.
- **Fix:** none needed. The warning is harmless.

### RestoreTest stays at `BackupNotFound`

- **Cause:** no `Completed` `Backup` with the selected labels covers the namespace.
- **Fix:** check `kubectl -n velero get backup --show-labels`, the phase, and
  `spec.includedNamespaces` / `spec.excludedNamespaces` of the newest `Backup`.

### Restore fails with `RestoredVolumeMissing`

- **Cause:** a `sourcePVC` was not in the `Backup`, or `spec.velero` filtered it out.
- **Fix:** check that the PVC carried the selected labels when the backup ran.
