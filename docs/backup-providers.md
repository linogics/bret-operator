# Backup providers

bret-operator restores with a backup tool that you already run. This page shows the setup that
the kind spike verified. Each provider needs a backup that the tool can restore, and a
`RestoreCheck` that checks the result.

The examples use an S3-compatible endpoint named `s3.example.internal`. Replace it with your
own. The spike used Versity Gateway as the S3 endpoint, which is open source. MinIO's open
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

The spike used `csi-driver-host-path` v1.19.0 with external-snapshotter v8.7.0. For production,
use the snapshot controller and driver that your storage vendor documents.

## Velero

Verified with Velero v1.18.4 and the AWS plugin v1.14.4.

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
velero backup create orders-db-2026-11-14 \
  --include-namespaces data \
  --snapshot-move-data \
  --wait
```

**Required:** `--snapshot-move-data`. Without it, the volume data stays in the cluster as a
snapshot. A restore then works only while that snapshot exists. Off-cluster recovery does not
work. The spike confirmed this.

Enable the provider in the chart with `providers.velero.enabled: true`. The Velero install
namespace is `providers.velero.namespace`, default `velero`. The chart passes it to the operator
as `VELERO_NAMESPACE`.

### What the operator drives

The operator creates a `velero.io/v1` `Restore` in the Velero namespace. The PVC and PVs are
restored into the scratch namespace `bret-scratch-<hash>` (hash of `<namespace>/<name>`). The check
pod runs in that namespace. The `Restore` sets `includedNamespaces` to the `Restore`
namespace, so Velero restores no other namespace of the backup. The operator copies the
`pod-security.kubernetes.io/*` labels of the `Restore` namespace onto the scratch namespace. The
operator waits for `status.phase` to reach `Completed`, `PartiallyFailed` or `Failed`.

Checks for Velero must not set `serviceAccountName`, `imagePullSecrets`, or configMap, secret
or hostPath volumes. A check that does fails with `ProviderUnsupported`.

Restored workloads are a trap. Velero restores Deployments and StatefulSets as well. They
start and hold the restored RWO volume. Scale them to zero, or delete them, before the check
pod runs. The spike hit this.

The restore log shows a harmless warning: `ConfigMap:kube-root-ca.crt already exists`.

## VolSync

Verified with VolSync 0.17.0 and restic.

### Install

```bash
helm repo add backube https://backube.github.io/helm-charts/
helm install volsync backube/volsync --namespace volsync-system --create-namespace
```

### Back up

Store the repository settings in a Secret in the application namespace:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: restic-config
  namespace: data
type: Opaque
stringData:
  RESTIC_REPOSITORY: "s3:https://s3.example.internal/volsync/orders-db"
  RESTIC_PASSWORD: "<a strong password you keep safe>"
  AWS_ACCESS_KEY_ID: "<access key>"
  AWS_SECRET_ACCESS_KEY: "<secret key>"
---
apiVersion: volsync.backube/v1alpha1
kind: ReplicationSource
metadata:
  name: orders-db-backup
  namespace: data
spec:
  sourcePVC: orders-db-data
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

For a database, set the mover's UID to the database's UID with `moverSecurityContext`.
Postgres 17 uses 999. See [data-and-database.md](data-and-database.md).

Enable the provider in the chart with `providers.volsync.enabled: true`.

### What the operator drives

The operator creates a `ReplicationDestination` in the restore namespace with a restic mover. The
repository Secret name and the source PVC come from the backup's `ReplicationSource`, in the backup
namespace. The operator uses `backupRef.namespace`. If that is not set, it uses the `Restore` namespace.
The Secret itself must exist in the `Restore` namespace. Without `spec.restic.repository` on the
`ReplicationSource`, the `Restore` shows the reason `RepositoryMissing`.
The restore is done when `status.lastManualSync` equals the trigger value.

Mover pods use a default non-database UID. They cannot read a Postgres data directory
(`permission denied`). The operator takes the mover UID from the check (annotation
`bret.linogics.io/mover-uid`, else the first container's `runAsUser`). It uses 65534 when the
check sets neither.

A VolSync cache volume made by an earlier mover UID causes `permission denied` after the UID
changes. Delete the cache PVC. VolSync then creates a new one.

## Kopiur (alpha)

Verified with Kopiur 0.10.21. Kopiur is alpha. Its restore-related issue `kopiur#233` was not
hit in the spike, but it is still open.

Enable the provider in the chart with `providers.kopiur.enabled`. It is on by default.

### Repository and policy

Kopiur stores repository credentials in a Secret. Put the S3 keys and the Kopia password in it:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: kopiur-repo-creds
  namespace: data
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
  namespace: data
spec:
  backend:
    s3:
      bucket: kopiur
      prefix: orders-db/
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
  name: orders-db-data
  namespace: data
spec:
  repository:
    kind: Repository
    name: primary
  sources:
    - pvc:
        name: orders-db-data
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

The `mover` block is needed for databases. Without it, the mover reads the data directory as
UID 65532 and fails with `permission denied`. Set the UID to the database's UID.

Take a snapshot by hand:

```yaml
apiVersion: kopiur.home-operations.com/v1alpha1
kind: Snapshot
metadata:
  name: orders-db-data-1
  namespace: data
spec:
  policyRef:
    name: orders-db-data
  deletionPolicy: Retain
```

Wait until `status.phase` is `Succeeded`.

### Restore

A Kopiur restore runs in a namespace. Copy the repository credentials Secret into that
namespace first. Kopiur did not find the Secret in another namespace, and it failed with
`MissingCredentialsSecret`.

```yaml
apiVersion: kopiur.home-operations.com/v1alpha1
kind: Restore
metadata:
  name: orders-db-restore
  namespace: restore-test
spec:
  source:
    snapshotRef:
      name: orders-db-data-1
      namespace: data
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
  name: orders-db-restored
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
    name: orders-db-restore
```

Use your own `StorageClass` for the PVC. The spike used `csi-hostpath-sc`.

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
