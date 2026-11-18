---
title: Failure reasons
description: Every failure reason bret reports, where it shows, what it means and how to fix it.
---

bret reports why something stopped in the `Ready` condition of the object, in its
`status.message`, and as a Warning Event with the same reason. Read them with:

```bash
kubectl -n <namespace> describe restore <name>
kubectl -n <namespace> describe restoretest <name>
```

Reasons that only mark progress (`RestoreStarted`, `CheckPassed`, `RunSkipped` and others) are
on [Metrics and events](/reference/metrics-and-events/).

## How a reason ends

| Ends the `Restore` as `Failed` | Waits and retries on its own | Blocks the `RestoreTest` |
|---|---|---|
| `SourceMissing`, `SourceMismatch`, `BackupUnsupported`, `RestoredVolumeMissing`, `RepositoryMissing`, `CheckRefMissing`, `SizeUnknown`, `InsufficientCapacity`, `RestoreFailed`, `ProviderUnsupported` (Velero check) | `CheckNotFound`, `CheckUnsupported`, `ProviderUnsupported` (provider disabled), `RestoreNameInUse` | `BackupNotFound`, `BackupSelectorAmbiguous`, `InvalidCron`, `ProviderUnsupported`, `SourceMissing` |

A `Failed` attempt of a `RestoreTest` is retried until `retries.count` runs out. Fix the cause
first, or every retry fails the same way. `RestoreCanceled` ends the attempt as `Canceled` and
is not retried.

## `BackupNotFound`

- **Shows on:** `RestoreTest` `Ready=False`. With VolSync and `restoreAsOf`, also on a `Restore`,
  which ends `Failed`.
- **Means:** a source has no finished backup that matches its `backupSelector`. The message names
  the sources. With VolSync, restic found no snapshot at or before `restoreAsOf`.
- **Fix:** check the selector and the backups. What counts as finished differs per tool: see
  "Know which backup bret picks" on [Velero](/providers/velero/existing-deployment/#2-know-which-backup-bret-picks),
  [VolSync](/providers/volsync/existing-deployment/#4-know-which-backup-bret-picks) and
  [Kopiur](/providers/kopiur/existing-deployment/#4-know-which-snapshot-bret-picks).

## `BackupSelectorAmbiguous`

- **Shows on:** `RestoreTest` `Ready=False`. VolSync only.
- **Means:** the `labels` of a source match more than one `ReplicationSource`. The message names
  the matches.
- **Fix:** use `backupSelector.name`, or labels that only one `ReplicationSource` carries.
  See [VolSync limitations](/providers/volsync/limitations/).

## `BackupUnsupported`

- **Shows on:** `Restore`, ends `Failed` before Velero starts. Velero only.
- **Means:** the `Backup` has `snapshotVolumes: false`, or a source PVC is in it only as file
  system backup. Velero writes file system backup data only while it restores the pod, and bret
  restores no pods, so the PVC would come back empty.
- **Fix:** back the PVC up with CSI snapshots, best with `snapshotMoveData`. See
  [Volume data must be a snapshot](/providers/velero/existing-deployment/#1-volume-data-must-be-a-snapshot).

## `CheckNotFound`

- **Shows on:** `Restore` `Ready=False`. It retries until the check exists.
- **Means:** the `RestoreCheck` of `restoreCheckRef` is not in the namespace of the `Restore`.
- **Fix:** create the check in that namespace, or fix the name.

## `CheckRefMissing`

- **Shows on:** `Restore`, ends `Failed`. Velero only.
- **Means:** a Secret or ConfigMap that the check names in `imagePullSecrets`, `env` or `envFrom`
  is not in the `Restore` namespace, so bret cannot copy it into the scratch namespace.
- **Fix:** create it in the `Restore` namespace, or mark an env reference `optional: true`.

## `CheckUnsupported`

- **Shows on:** `Restore` `Ready=False`. It retries, so a fixed check is picked up.
- **Means:** the check is invalid: `restoredVolumes` is empty, a `sourcePVC` is used twice or is
  not a DNS label, a `sourcePVC` clashes with a scratch volume claim, the check pod cannot be
  built, or the `bret.linogics.io/mover-uid` annotation is not a JSON map. The same error shows
  as `Ready=False`, reason `Invalid`, on the `RestoreCheck`.
- **Fix:** read the message and fix the check. `kubectl get restorecheck` shows the result.

## `InsufficientCapacity`

- **Shows on:** `Restore`, ends `Failed`.
- **Means:** `size` is smaller than the size of the source.
- **Fix:** raise `size`, or drop it and let `autoExpand` size the volume. See
  [Scratch volume size](/concepts/volumes/#scratch-volume-size).

## `InvalidCron`

- **Shows on:** `RestoreTest` `Ready=False`.
- **Means:** `triggers.cron` does not parse. The message quotes it.
- **Fix:** use a standard 5-field cron expression.

## `ProviderUnsupported`

- **Shows on:** `Restore` or `RestoreTest` `Ready=False`.
- **Means:** one of two things.
  - The provider of `spec.provider` is disabled in the chart (`providers.<name>.enabled`). The
    object waits.
  - Velero: the check uses `serviceAccountName`, `hostPath`, or a configMap, secret, projected or
    persistentVolumeClaim volume. Those do not work in the scratch namespace. The `Restore` ends
    `Failed`.
- **Fix:** enable the provider, or pass Secrets to a Velero check with `env` or `envFrom`. See
  [Velero limitations](/providers/velero/limitations/).

## `RepositoryMissing`

- **Shows on:** `Restore`, ends `Failed`. VolSync only.
- **Means:** the `ReplicationSource` has no `spec.restic.repository`, or that Secret is not in the
  `Restore` namespace.
- **Fix:** keep the repository Secret next to the `Restore`. See
  [VolSync limitations](/providers/volsync/limitations/).

## `RestoreCanceled`

- **Shows on:** `Restore`, ends `Canceled`. Event reason.
- **Means:** `restoreWindow` ran out before the restore and the check finished.
- **Fix:** raise `restoreWindow` for large volumes or slow image pulls. See
  [restoreWindow](/concepts/lifecycle/#restorewindow).

## `RestoreFailed`

- **Shows on:** `Restore`, ends `Failed`. Event reason.
- **Means:** the backup tool reported a failed restore (Velero `Failed`, `PartiallyFailed` or
  `FailedValidation`, Kopiur `Failed`), or the check pod exited non-zero. The message says which.
- **Fix:** for a failed check, read the check pod logs. Keep the pod with `keepOnFailure: true`.
  For a tool failure, read the native object named in `status.providerRefs`.

## `RestoreNameInUse`

- **Shows on:** `RestoreTest` `Ready=False`. It retries on its own.
- **Means:** the name of the next attempt is taken by a `Restore` that is being deleted, or that
  another `RestoreTest` owns.
- **Fix:** usually none. The run starts once that `Restore` is gone.

## `RestoredVolumeMissing`

- **Shows on:** `Restore`, ends `Failed`. Velero only.
- **Means:** a `sourcePVC` was not restored, or the Velero `Restore` completed without restoring
  any item. The `spec.velero` selector did not match the PVC, or the `Backup` lacks it.
- **Fix:** label the PVC so the selector matches, and take a new backup. See
  [Velero limitations](/providers/velero/limitations/).

## `SizeUnknown`

- **Shows on:** `Restore`, ends `Failed`.
- **Means:** `size` is not set and the source size is unknown, so bret cannot size the scratch
  volume.
- **Fix:** set `size` on the source. See [Scratch volume size](/concepts/volumes/#scratch-volume-size).

## `SourceMismatch`

- **Shows on:** `Restore`, ends `Failed`. Kopiur and VolSync.
- **Means:** the backup holds another PVC than the `sourcePVC` of its source. A Kopiur
  `SnapshotPolicy` with several `pvc:` entries backs up only the first one.
- **Fix:** point the source at the backup of that PVC. With Kopiur, use one policy per PVC.

## `SourceMissing`

- **Shows on:** `Restore`, ends `Failed`. On a `RestoreTest`, `Ready=False` when it has no
  sources.
- **Means:** the `sourcePVC` values of the check (`restoredVolumes`) and of the `Restore` or
  `RestoreTest` (`sources`) differ, or the object has no sources at all.
- **Fix:** add the missing entry on either side. See
  [Which PVC gets restored](/concepts/volumes/#which-pvc-gets-restored).

## Nothing happens after a RestoreTest is created

- No condition and no Events: the namespace is not in `watchNamespaces`, so the operator ignores
  the object. Or `spec.triggers` is not set, or no trigger is due yet. Wait, or start a manual
  run. See [Schedule restore tests](/guides/restore-tests/#triggers).
- `BackupsPending` is `True` with reason `WaitingForBackups`: with `afterBackup`, some sources
  have a new backup and others do not yet. The run starts when every source has one.
- A source mismatch is not a `Ready` reason of the `RestoreTest`. The run ends `Failed` and
  `status.lastRun.reason` carries the message of the `Restore`.

## Errors inside check pods

### Postgres: `Permissions should be u=rwx (0700) or u=rwx,g=rx (0750)`

- **Cause:** the restored data directory has the wrong mode. A pod `fsGroup` changed it to `2770`.
- **Fix:** remove `fsGroup` from the database container. Or run a `pgdata-perms` init container
  that runs `chmod 0700 "$PGDATA"` as the database UID. The Postgres checks do this.

### Postgres: `another server might be running; trying to start server anyway`

- **Cause:** a crash-consistent copy keeps `postmaster.pid`. Postgres checks whether that PID is
  alive, finds a different process, and starts anyway.
- **Fix:** none needed. The message is a warning. The check still starts the server.

### Postgres: `pg_amcheck: error: no relations to check`

- **Cause:** `amcheck` is not installed in any database in the scratch copy. `pg_amcheck` finds
  nothing to check and exits 1.
- **Fix:** run `create extension if not exists amcheck` in each database before `pg_amcheck`. The
  scratch copy is the right place for this. Do not change the source.

Errors of the backup tools themselves are on their pages:
[Velero](/providers/velero/limitations/#troubleshooting),
[VolSync](/providers/volsync/limitations/#troubleshooting),
[Kopiur](/providers/kopiur/limitations/#troubleshooting).
