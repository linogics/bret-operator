# Backup providers

bret restores with a backup tool that you already run. This page shows setup that was
tested on a kind cluster. Each provider needs a backup that the tool can restore, and a
`RestoreCheck` that checks the result.

The examples use an S3-compatible endpoint named `s3.example.internal`. Replace it with your
own. The tests used Versity Gateway as the S3 endpoint, which is open source. MinIO's open
source distribution is no longer maintained, so this project does not use it.

Use one bucket or prefix per tool. Do not share a repository between tools.

## Before any provider: CSI snapshots

All three providers below capture volumes with CSI snapshots (`copyMethod: Snapshot`). Check
that your cluster supports them:

```bash
kubectl get volumesnapshotclass
kubectl get storageclass
```

You need one `VolumeSnapshotClass` that uses your CSI driver. Note its name. The examples call
it `csi-snapclass`. Each backup tool also needs a `StorageClass` that the driver supports.

Tested with `csi-driver-host-path` v1.19.0 and external-snapshotter v8.7.0. For production,
use the snapshot controller and driver that your storage vendor documents.

## Velero

Tested with Velero v1.18.4 and the AWS plugin v1.14.4.

### Install

```bash
velero install \
  --provider aws \
  --plugins velero/velero-plugin-for-aws:v1.14.4 \
  --image velero/velero:v1.18.4 \
  --bucket velero \
  --secret-file ./velero-credentials \
  --backup-location-config "region=us-east-1,s3ForcePathStyle=true,s3Url=https://s3.example.internal" \
  --use-volume-snapshots=false \
  --features=EnableCSI \
  --wait
```

`velero-credentials` is an AWS-style credentials file:

```ini
[default]
aws_access_key_id = <access key>
aws_secret_access_key = <secret key>
```

Do not commit this file.

### Back up with data movement

```bash
velero backup create nextcloud-2026-11-14 \
  --include-namespaces nextcloud \
  --snapshot-move-data \
  --wait
```

**Required:** `--snapshot-move-data`. Without it, the volume data stays in the cluster as a
snapshot. A restore then works only while that snapshot exists. Off-cluster recovery does not
work.

Label every app PVC before the backup, so a restore can select it. Velero has no name filter. A
selector matches the labels that are stored in the backup, so a label added after the backup does
not count:

```bash
kubectl -n nextcloud label pvc html pgdata app.kubernetes.io/name=nextcloud
```

A Velero `Restore` source has no `sourcePVC` and no size. It takes one `backupRef` and one of two
selectors, which are mutually exclusive:

- `labelSelector`: a standard label selector. One selector matches all the PVCs.
- `orLabelSelectors`: a list of selectors. A PVC matches if any one matches. Use it when the PVCs
  carry different labels.

Every `sourcePVC` of the check must be among the restored PVCs. Otherwise the `Restore` ends Failed with
`RestoredVolumeMissing`.

Enable the provider in the chart with `providers.velero.enabled: true`. The Velero install
namespace is `providers.velero.namespace`, default `velero`. The chart passes it to the operator
as `VELERO_NAMESPACE`.

A manual `Restore` for Velero: [examples/restore-velero.yaml](../examples/restore-velero.yaml)
(one selector) and [examples/restore-velero-multi.yaml](../examples/restore-velero-multi.yaml)
(`orLabelSelectors`).

### What the operator drives

The namespaces, the scratch namespace `bret-scratch-<hash>` and the list of what a Velero check
must not set are in [concepts.md](concepts.md#velero). Only the facts below are specific to this
page.

The operator reads the Velero `Restore` `status.phase`. `Completed` runs the check. `Failed`,
`PartiallyFailed` and `FailedValidation` fail the attempt.

A check that needs a Secret must use Kopiur or VolSync. See [checks.md](checks.md#secrets).

Restored workloads are a trap. Velero restores Deployments and StatefulSets as well. They
start and hold the restored RWO volume. Scale them to zero, or delete them, before the check
pod runs.

The restore log shows a harmless warning: `ConfigMap:kube-root-ca.crt already exists`.

## VolSync

Tested with VolSync 0.17.0 and restic.

### Install

```bash
helm repo add backube https://backube.github.io/helm-charts/
helm install volsync backube/volsync --namespace volsync-system --create-namespace
```

### Back up

VolSync has one `ReplicationSource` per PVC. An app with two PVCs, such as nextcloud, needs two.
Each `Restore` source then names one of them.

Store the repository settings in a Secret in the application namespace:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: restic-config
  namespace: nextcloud
type: Opaque
stringData:
  RESTIC_REPOSITORY: "s3:https://s3.example.internal/volsync/nextcloud"
  RESTIC_PASSWORD: "<a strong password you keep safe>"
  AWS_ACCESS_KEY_ID: "<access key>"
  AWS_SECRET_ACCESS_KEY: "<secret key>"
---
apiVersion: volsync.backube/v1alpha1
kind: ReplicationSource
metadata:
  name: html-backup
  namespace: nextcloud
spec:
  sourcePVC: html
  trigger:
    schedule: "0 2 * * *"
  restic:
    repository: restic-config
    copyMethod: Snapshot
    volumeSnapshotClassName: csi-snapclass
    pruneIntervalDays: 7
    retain:
      daily: 7
---
apiVersion: volsync.backube/v1alpha1
kind: ReplicationSource
metadata:
  name: pgdata-backup
  namespace: nextcloud
spec:
  sourcePVC: pgdata
  trigger:
    schedule: "0 2 * * *"
  restic:
    repository: restic-config
    copyMethod: Snapshot
    volumeSnapshotClassName: csi-snapclass
    pruneIntervalDays: 7
    retain:
      daily: 7
```

The operator reads `spec.sourcePVC` of each `ReplicationSource`. If it is not the `sourcePVC` of
the `Restore` source, the `Restore` ends Failed with `SourceMismatch`.

For a database, set the mover's UID to the database's UID with `moverSecurityContext`.
Postgres 17 uses 999. Set the UID per volume with the `mover-uid` annotation on the check, keyed by
`sourcePVC` (see [checks.md](checks.md#mover-uid)). See [data-and-database.md](data-and-database.md).

Enable the provider in the chart with `providers.volsync.enabled: true`.

A manual `Restore` for VolSync, with one source per PVC: [examples/restore-volsync.yaml](../examples/restore-volsync.yaml).

### What the operator drives

The namespaces, the repository Secret, the reason `RepositoryMissing` and the order of the mover
UID are in [concepts.md](concepts.md#volsync). Only the facts below are specific to this page.

The check runs when the `ReplicationDestination` reports `status.latestImage.kind: PersistentVolumeClaim`.

Mover pods use a default non-database UID. They cannot read a Postgres data directory
(`permission denied`). So set the mover UID on the check.

A VolSync cache volume made by an earlier mover UID causes `permission denied` after the UID
changes. Delete the cache PVC. VolSync then creates a new one.

## Kopiur (alpha)

Tested with Kopiur 0.10.21. Kopiur is alpha. Upstream issue `kopiur#233` affects restores and is
still open.

Enable the provider in the chart with `providers.kopiur.enabled`. It is on by default.

### Repository and policy

Kopiur stores repository credentials in a Secret. Put the S3 keys and the Kopia password in it:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: kopiur-repo-creds
  namespace: nextcloud
type: Opaque
stringData:
  AWS_ACCESS_KEY_ID: "<access key>"
  AWS_SECRET_ACCESS_KEY: "<secret key>"
  KOPIA_PASSWORD: "<a strong password you keep safe>"
---
apiVersion: kopiur.home-operations.com/v1alpha1
kind: Repository
metadata:
  name: primary
  namespace: nextcloud
spec:
  backend:
    s3:
      bucket: kopiur
      prefix: nextcloud/
      endpoint: s3.example.internal
      region: us-east-1
      auth:
        secretRef:
          name: kopiur-repo-creds
  encryption:
    passwordSecretRef:
      name: kopiur-repo-creds
      key: KOPIA_PASSWORD
  create:
    enabled: true
---
apiVersion: kopiur.home-operations.com/v1alpha1
kind: SnapshotPolicy
metadata:
  name: pgdata
  namespace: nextcloud
spec:
  repository:
    kind: Repository
    name: primary
  sources:
    - pvc:
        name: pgdata
  copyMethod: Snapshot
  volumeSnapshotClassName: csi-snapclass
  retention:
    keepDaily: 7
  mover:
    securityContext:
      runAsUser: 999
      runAsGroup: 999
      runAsNonRoot: true
```

**Warning: one Snapshot holds one PVC.** A `SnapshotPolicy` with several `pvc:` sources backs up
only the first one. The others are silently skipped. Use one policy per PVC (as `pgdata` above, and
a second policy `html`), or a policy with `pvcSelector`, which makes one `Snapshot` per matching
PVC:

```yaml
  sources:
    - pvcSelector:
        matchLabels:
          app.kubernetes.io/name: nextcloud
```

Each `Restore` source names the `Snapshot` of one PVC. If the `Snapshot` holds another PVC than the
`sourcePVC` of the source, the `Restore` ends Failed with `SourceMismatch`.

The `mover` block is needed for databases. Without it, the mover reads the data directory as
UID 65532 and fails with `permission denied`. Set the UID to the database's UID.

Take a snapshot by hand:

```yaml
apiVersion: kopiur.home-operations.com/v1alpha1
kind: Snapshot
metadata:
  name: pgdata-1
  namespace: nextcloud
spec:
  policyRef:
    name: pgdata
  deletionPolicy: Retain
```

Wait until `status.phase` is `Succeeded`. Repeat for `html`.

A manual `Restore` for Kopiur, with one source per PVC: [examples/restore-kopiur.yaml](../examples/restore-kopiur.yaml).

### Restore

This is the Kopiur-native restore of one PVC, as the tool does it. The operator creates these
objects for you, one per source.

A Kopiur restore runs in a namespace. Copy the repository credentials Secret into that
namespace first. Kopiur did not find the Secret in another namespace. Without it, the restore
fails with `MissingCredentialsSecret`.

```yaml
apiVersion: kopiur.home-operations.com/v1alpha1
kind: Restore
metadata:
  name: nextcloud-pgdata-restore
  namespace: restore-test
spec:
  source:
    snapshotRef:
      name: pgdata-1
      namespace: nextcloud
  target:
    populator: {}
  mover:
    securityContext:
      runAsUser: 999
      runAsGroup: 999
      runAsNonRoot: true
  policy:
    onMissingSnapshot: Fail
    waitTimeout: 5m
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: nextcloud-pgdata-restored
  namespace: restore-test
spec:
  storageClassName: <your-storage-class>
  accessModes: ["ReadWriteOnce"]
  resources:
    requests:
      storage: 2Gi
  dataSourceRef:
    apiGroup: kopiur.home-operations.com
    kind: Restore
    name: nextcloud-pgdata-restore
```

Use your own `StorageClass` for the PVC. The tests used `csi-hostpath-sc`.

The restore is done when the `Restore` has `status.phase: Completed`, and its condition `Ready` has reason
`RestoreSucceeded`. Kopiur says `Completed`, not `Succeeded`. Do not confuse these words with the
operator's phases.

## Which provider to choose

| Need | Pick |
|---|---|
| Whole-namespace backups, many resource types, one tool for the cluster | Velero |
| Per-volume backups with restic, scheduled by the volume | VolSync |
| Per-volume backups with Kopia, deduplication, and a repository you manage | Kopiur (alpha) |

All three work with the check. The check does not care which tool made the data.
