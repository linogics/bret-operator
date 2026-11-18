---
title: Kopiur limitations
description: What bret cannot do with Kopiur, and fixes for known errors.
---

## Troubleshooting

### Kopiur restore: `MissingCredentialsSecret`

- **Cause:** the restore runs in a namespace without the repository credentials Secret.
- **Fix:** keep the Secret in the namespace of the `Restore`. See
  [The Kopiur-native restore](/providers/kopiur/walkthrough/#the-kopiur-native-restore).

### Kopiur snapshot: `PermissionDenied`, `cannot create iterator: unable to read directory`

- **Symptom:** `kopia snapshot create ... permission denied` on a database directory.
- **Cause:** the snapshot mover runs as UID 65532 by default. The data directory is owned by the
  database UID with mode 0700.
- **Fix:** set `spec.mover.securityContext.runAsUser` on the `SnapshotPolicy` to the database UID
  (999 for MariaDB and Postgres 17).

### Kopiur snapshot stays `Pending`, `WaitingForStagedPvcBind`

- **Symptom:** the condition says the staged PVC is waiting to bind. `kubectl get pvc` shows the
  staged PVC `Bound` a few seconds later.
- **Cause:** a reconcile lag. The controller retries on its own.
- **Fix:** wait. If it stays Pending past the staging deadline in the condition message, read the
  controller logs in `kopiur-system`.

### RestoreTest stays at `BackupNotFound`

- **Cause:** no `Succeeded` `Snapshot` matches the `backupSelector` of the named source.
- **Fix:** check the labels with `kubectl get snapshot --show-labels`, and the phase.
