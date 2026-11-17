# Backup providers

bret restores with a backup tool that you already run: Kopiur, VolSync or Velero. Each tool has
its own page. Every page walks through the same WordPress + MariaDB example, which has two PVCs:

- `html`: the WordPress docroot, `wp-config.php` and `wp-content`. Owned by uid 33.
- `mysql`: the MariaDB data dir. Owned by uid 999.

| Page | Backup object | One backup holds |
|---|---|---|
| [kopiur.md](kopiur.md) (alpha) | Kopiur `Snapshot` | one PVC |
| [volsync.md](volsync.md) | VolSync `ReplicationSource` (its last sync) | one PVC |
| [velero.md](velero.md) | Velero `Backup` | every PVC of the namespace |

Each page has the same steps:

1. Deploy WordPress + MariaDB.
2. Back up: a CSI snapshot of each PVC, then a copy to S3.
3. Write the check.
4. Restore by hand, then create a `RestoreTest`.
5. How bret picks backups.
6. Troubleshooting.

The example files are in [examples/](../../examples/): `examples/wordpress/` is the app and the
check, `examples/<tool>/` adds the backup setup, a `Restore`, a `RestoreTest`, and a negative
`Restore` under `negative/`. The files were tested on a kind cluster with
`hack/check-test/operator-roundtrip.sh <tool>` (see [testing-checks.md](../testing-checks.md)).

## Before any tool: CSI snapshots

All three tools capture volumes with CSI snapshots. Check that your cluster supports them:

```bash
kubectl get volumesnapshotclass
kubectl get storageclass
```

You need one `VolumeSnapshotClass` for your CSI driver and a `StorageClass` that the driver
serves. The examples use the kind dev cluster names `csi-hostpath-snapclass` and
`csi-hostpath-sc`. Replace them in the files, or patch them with a kustomize overlay.

Tested with `csi-driver-host-path` v1.19.0 and external-snapshotter v8.7.0. For production, use
the snapshot controller and driver that your storage vendor documents.

## S3

Every tool copies the snapshot data to S3. Use one bucket, or one prefix, per tool. Do not share
a repository between tools.

The docs use the endpoint `s3.example.internal`. Replace it with your own. The example files use
`versitygw.storage.svc.cluster.local:7070`, the endpoint of the kind dev cluster: Versity Gateway
with the posix backend and the buckets `kopiur`, `velero` and `volsync`. MinIO's open source
distribution is no longer maintained, so this project does not use it.

Keep S3 keys and repository passwords out of git. Create the Secrets with kubectl, for example:

```bash
kubectl -n wordpress create secret generic kopiur-repo-creds \
  --from-literal=AWS_ACCESS_KEY_ID=<access key> \
  --from-literal=AWS_SECRET_ACCESS_KEY=<secret key> \
  --from-literal=KOPIA_PASSWORD=<a strong password you keep safe>
```

Each tool page names the Secret and keys it needs.

## Point in time across PVCs

Kopiur and VolSync back up each PVC on its own. The `html` and `mysql` backups of one run can be
minutes apart, so the restored pair is not one point in time. A check that compares the two, as
the WordPress check does, can then fail on a write that happened between the backups. Back up
the PVCs close together, or quiesce the app during the backup.

Velero backs up all PVCs of the namespace in one `Backup`. Its PVCs are one point in time.

## Throttle restore throughput

A restore test reads the whole backup from S3 and writes it to a volume. It can saturate the
network or the disks. bret has no throttle field. Each tool has its own levers. Checked against
the tool docs in November 2026.

### Kopiur

- Kopia throttle: `Repository.spec.moverDefaults.throttle` with `downloadBytesPerSecond`,
  `uploadBytesPerSecond`, `readOpsPerSecond`, `writeOpsPerSecond`.
  - Pro: a real bandwidth and ops cap. Restore movers honor it. No CNI support needed.
  - Con: set per repository, so it also slows backup, maintenance and replication movers.
  - Con: no per-`Restore` field.
  - Con: cached content bypasses the cap. Small objects stay well below the cap.
- Restore parallelism: `Restore.spec.options.parallel`, default `8`.
  - Pro: set per restore.
  - Con: limits concurrency, not bytes or ops.
- Mover pod annotations: `Repository.spec.moverDefaults.podAnnotations`.
  - Pro: carries the `kubernetes.io/ingress-bandwidth` annotation.
  - Con: set per repository, so it hits every mover. Needs a CNI that enforces ingress limits.

### Velero

- No bandwidth or IOPS cap.
- Files in parallel: `velero restore create --parallel-files-download=<n>`.
  - Pro: set per restore.
  - Con: limits concurrency only. Only the file-system data mover. No effect on block mode.
- node-agent ConfigMap `loadConcurrency` (`globalConfig`, `perNodeConfig`).
  - Pro: limits the data mover pods per node.
  - Con: set per install. Also limits backups. Needs a node-agent restart.
- node-agent ConfigMap `podResources` (`cpuLimit`, `memoryLimit`).
  - Pro: caps CPU of the restore pods.
  - Con: an indirect brake, not a throughput cap. Set per install.
- node-agent ConfigMap `podAnnotations` / `podLabels` (`PodVolumeRestore` and `DataDownload`
  pods).
  - Pro: carries the `kubernetes.io/ingress-bandwidth` annotation.
  - Con: set per install, so it also hits backups. Replaces Velero's own annotations, such as IAM
    role annotations, so add those back. Needs a CNI that enforces ingress limits.

### VolSync

- No bandwidth or IOPS cap.
- `moverResources` (CPU, memory) on the mover spec.
  - Pro: set per `ReplicationDestination`, so only the restore.
  - Con: an indirect brake, not a throughput cap. Tight limits can crash the mover.
- `moverPodLabels`.
  - Pro: set per `ReplicationDestination`.
  - Con: labels only, no annotations, so the bandwidth annotation cannot be set.
- Restic env from the repository Secret.
  - Con: the docs name no bandwidth variable.

### Pod bandwidth annotation

- `kubernetes.io/ingress-bandwidth` on the mover pod caps the download.
  - Pro: the same mechanism for every tool.
  - Con: works only if the CNI enforces it, for example the CNI `bandwidth` plugin. Cilium ignores
    the ingress annotation.
  - Con: no tool can set it per restore. VolSync cannot set it at all. A mutating webhook would be
    needed to inject it into restore pods only.
- Kubernetes has no per-pod disk IOPS limit. Use storage QoS (StorageClass parameters) instead.

## Which tool to choose

| Need | Pick |
|---|---|
| Whole-namespace backups, many resource types, one tool for the cluster | Velero |
| Per-volume backups with restic, scheduled by the volume | VolSync |
| Per-volume backups with Kopia, deduplication, and a repository you manage | Kopiur (alpha) |

All three work with the same check. The check does not care which tool made the data.

Enable a tool in the Helm chart with `providers.<name>.enabled`. Kopiur is on by default. Velero
and VolSync are off by default. A `Restore` or `RestoreTest` for a disabled tool stops with reason
`ProviderUnsupported`.
