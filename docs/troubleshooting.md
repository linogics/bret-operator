# Troubleshooting

Each entry gives the exact text you will see, the cause, and the fix. Errors come from the
spike on kind with Kubernetes 1.37, Velero 1.18.4, VolSync 0.17.0, Kopiur 0.10.21 and
PostgreSQL 17.12. See [spike-findings.md](spike-findings.md).

## Backups and restores

### Velero backup is gone after the source cluster is lost

- **Symptom:** the restore works only while the snapshot still exists in the cluster.
- **Cause:** the backup was created without data movement. Velero kept the volume data as a
  cluster snapshot.
- **Fix:** create backups with `velero backup create ... --snapshot-move-data`. See
  [backup-providers.md](backup-providers.md#velero).

### Restore warning `ConfigMap:kube-root-ca.crt already exists`

- **Cause:** every namespace already has this ConfigMap.
- **Fix:** none needed. It is harmless.

### Restored Deployment holds the volume, check pod stays Pending

- **Symptom:** the check pod waits with a volume attach error for an RWO PVC.
- **Cause:** a Velero restore brings back a Deployment that mounts the restored volume. It starts
  and holds the RWO volume.
- **Fix:** scale restored workloads to zero, or delete them, before the check runs.

### Kopiur restore: `MissingCredentialsSecret`

- **Cause:** the `Restore` is in a different namespace from the repository, and the credentials
  Secret is not there.
- **Fix:** copy the Secret into the restore namespace. See
  [backup-providers.md](backup-providers.md#restore).

### Kopiur snapshot: `PermissionDenied`, `cannot create iterator: unable to read directory`

- **Symptom:** `kopia snapshot create ... permission denied` on a database directory.
- **Cause:** the snapshot mover runs as UID 65532 by default. The data directory is owned by the
  database UID with mode 0700.
- **Fix:** set `spec.mover.securityContext.runAsUser` on the `SnapshotPolicy` to the database UID
  (999 for Postgres 17).

### Kopiur snapshot stays `Pending`, `WaitingForStagedPvcBind`

- **Symptom:** the condition says the staged PVC is waiting to bind. `kubectl get pvc` shows the
  staged PVC `Bound` a few seconds later.
- **Cause:** a reconcile lag. The controller retries on its own.
- **Fix:** wait. If it stays Pending past the staging deadline in the condition message, read the
  controller logs in `kopiur-system`.

### VolSync mover: `permission denied` on the data directory

- **Cause:** the mover runs as the default non-database UID.
- **Fix:** set `moverSecurityContext` in the `ReplicationSource` and `ReplicationDestination` to
  the database UID.

### VolSync: `permission denied` after changing the mover UID

- **Cause:** the cache PVC was created by the old mover UID.
- **Fix:** delete the cache PVC. VolSync creates a new one.

## Checks

### Postgres: `Permissions should be u=rwx (0700) or u=rwx,g=rx (0750)`

- **Cause:** the restored data directory has the wrong mode. A pod `fsGroup` changed it to `2770`.
- **Fix:** remove `fsGroup` from the database container. Or run a `pgdata-perms` init container
  that runs `chmod 0700 "$PGDATA"` as the database UID. The Postgres blueprints do this.

### Postgres: `another server might be running; trying to start server anyway`

- **Cause:** a crash-consistent copy keeps `postmaster.pid`. Postgres checks whether that PID is
  alive, finds a different process, and starts anyway.
- **Fix:** none needed. The message is a warning. The check still starts the server.

### Postgres: `pg_amcheck: error: no relations to check`

- **Cause:** `amcheck` is not installed in any database in the scratch copy. `pg_amcheck` finds
  nothing to check and exits 1.
- **Fix:** run `create extension if not exists amcheck` in each database before `pg_amcheck`. The
  scratch copy is the right place for this. Do not change the source.

## Development and cluster setup

### `kind` with podman fails: `permission denied` on `/dev/.incus-mounts`

- **Cause:** the kind node mounts host paths that a rootless podman setup on this host cannot
  reach.
- **Fix:** use the nerdctl provider. kind picks it when `nerdctl` is on the path with containerd.
  See [development.md](development.md).

### nerdctl network create fails with an overlapping subnet

- **Symptom:** `network ... overlaps` on an IPv6 range.
- **Cause:** an existing host bridge uses the same range.
- **Fix:** create the `kind` network yourself on a free range before creating the cluster.

## Operator

### `kubectl apply` of a `RestoreBlueprint` is rejected: `exactly one of restoreMount or restoredVolumes must be set`

- **Cause:** both fields are set, or neither is.
- **Fix:** keep exactly one. See [blueprints.md](blueprints.md#anatomy).

### Nothing happens after a `RestoreTest` is created

- **Cause:** the controllers are not implemented yet. See the status table in
  [README.md](README.md).
- **Fix:** run the check by hand with [testing-blueprints.md](testing-blueprints.md).
