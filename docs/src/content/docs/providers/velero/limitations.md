---
title: Velero limitations
description: What bret cannot do with Velero, and fixes for known errors.
---

## Limits

- **No file system backup.** A PVC saved with Velero file system backup (kopia or restic through
  the node agent) cannot be restored without its pod. bret refuses it with `BackupUnsupported`.
  Use CSI snapshots, best with `snapshotMoveData`.
- **Scratch namespace.** The check runs in `bret-scratch-<hash>`, not in your namespace. A check
  with `serviceAccountName`, `hostPath`, or a configMap, secret, projected or
  persistentVolumeClaim volume ends `Failed` with `ProviderUnsupported`.
- **Cluster-wide rights.** With Velero on, the operator can read and create Secrets and
  ConfigMaps and manage pods and PVCs in every namespace.
- **Velero picks size and storage class.** PVCs come back at their original size and storage
  class. A Velero source has no `size`, `autoExpand` or `storageClassName`.
- **One Backup per run.** Every source of a `Restore` names the same `backupRef`, and every
  source of a `RestoreTest` the same `backupSelector`.
- **Restored PVCs only.** Deployments, Services and other resources of the backup are not
  restored or tested.
- **Backups without data movement die with the cluster.** A CSI snapshot without
  `snapshotMoveData` lives in the cluster. bret can test it, but it does not prove off-site
  recovery.
- **Completed only.** `PartiallyFailed` backups are never picked.

## Troubleshooting

### Restore fails with `BackupUnsupported`

- **Cause:** the `Backup` has `snapshotVolumes: false`, or a source PVC was saved with file
  system backup.
- **Fix:** back the PVC up as a CSI snapshot. See
  [Volume data must be a snapshot](/providers/velero/existing-deployment/#1-volume-data-must-be-a-snapshot).
  Reason: [`BackupUnsupported`](/reference/failure-reasons/#backupunsupported).

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
  Reason: [`BackupNotFound`](/reference/failure-reasons/#backupnotfound).

### Restore fails with `RestoredVolumeMissing`

- **Cause:** a `sourcePVC` was not in the `Backup`, `spec.velero` filtered it out, or the
  Velero `Restore` completed without restoring any item.
- **Fix:** check that the PVC carried the selected labels when the backup ran, and
  `velero restore describe <name> --details`.
  Reason: [`RestoredVolumeMissing`](/reference/failure-reasons/#restoredvolumemissing).

### Restore fails with `CheckRefMissing`

- **Cause:** the check names a Secret or ConfigMap that is not in the `Restore` namespace, so
  bret cannot copy it.
- **Fix:** create it, or mark the reference `optional: true`.
  Reason: [`CheckRefMissing`](/reference/failure-reasons/#checkrefmissing).

### Restore fails with `RestoreFailed`

- **Cause:** the Velero `Restore` ended `Failed`, `PartiallyFailed` or `FailedValidation`.
- **Fix:** `velero restore describe <name> --details` and `velero restore logs <name>`. The name
  is in `status.providerRefs` of the `Restore`.
  Reason: [`RestoreFailed`](/reference/failure-reasons/#restorefailed).
