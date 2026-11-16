# Troubleshooting

Each entry gives the exact text you see, the cause, and the fix. Errors were seen on kind with
Kubernetes 1.37, Velero 1.18, VolSync 0.17.0, Kopiur 0.11.0 and PostgreSQL 17.12.

## Backups and restores

Errors of a backup tool are on its page: [Kopiur](providers/kopiur.md#6-troubleshooting),
[VolSync](providers/volsync.md#6-troubleshooting), [Velero](providers/velero.md#6-troubleshooting).

## Checks

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

## Operator

### A `Restore` ends Failed with a source reason

These three reasons end the `Restore` as `Failed` (`Ready` is `False`, a Warning Event is written,
no check pod runs). A `RestoreTest` retries it like any failed attempt, until `retries` run out.
Fix the cause first, or every retry fails the same way.

- **`SourceMissing`:** the `sourcePVC` values of the check (`restoredVolumes`) and of the
  `Restore` or `RestoreTest` (`sources`) differ. Add the missing entry on either side. See
  [concepts.md](concepts.md#which-pvc-gets-restored).
- **`SourceMismatch`** (Kopiur, VolSync): the backup holds another PVC than the `sourcePVC` of its
  source. Point the source at the backup of that PVC. A Kopiur `SnapshotPolicy` with several `pvc:`
  entries backs up only the first one. Use one policy per PVC or `pvcSelector`.
- **`RestoredVolumeMissing`** (Velero): a `sourcePVC` of the `Restore` was not restored. The
  `spec.velero` selector did not match the PVC, or the backup lacks it. Label the PVC and take a
  new backup.
- **`CheckRefMissing`** (Velero): a Secret or ConfigMap that the check names in
  `imagePullSecrets`, `env` or `envFrom` is not in the `Restore` namespace. Create it there, or
  mark an env reference `optional: true`.

### Nothing happens after a `RestoreTest` is created

- **Symptom:** no `Restore` appears. Read the reason of the `Ready` condition and the Events:
  `kubectl -n <namespace> describe restoretest <name>`.
- **Cause and fix**, by what you see:
  - No condition and no Events: the namespace is not in `watchNamespaces`, so the operator ignores
    the object. Add the namespace to the chart value. Or `spec.triggers` is not set, or no trigger
    is due yet. Then wait, or start a manual run. See
    [restoretest-guide.md](restoretest-guide.md#triggers).
  - `ProviderUnsupported`: the provider of `spec.provider` is disabled in the chart
    (`providers.<name>.enabled`).
  - `BackupNotFound`: a source has no finished backup that matches its `backupSelector`. The
    message names those sources. What counts as finished, and where the operator looks, is under
    "How bret picks backups" on the tool page in [providers/](providers/README.md).
  - `BackupSelectorAmbiguous` (VolSync): the `labels` of a source match more than one
    `ReplicationSource`. Use `name`, or labels that one `ReplicationSource` carries.
  - A source mismatch is not a `Ready` reason of the `RestoreTest`. The run ends `Failed` and
    `status.lastRun.reason` carries the `SourceMissing` message of the `Restore` (see above).

  The `BackupsPending` condition is separate from `Ready`. With `afterBackup`, it is `True`
  (reason `WaitingForBackups`) while some sources have a new backup and others do not yet. The
  run starts when every source has one.
- A `Restore` exists but does not progress: read its `Ready` condition. `ProviderUnsupported`
  means that its provider is disabled (`providers.<name>.enabled`). `CheckNotFound` means that the
  `RestoreCheck` of `restoreCheckRef` is not in the namespace of the `Restore`.
