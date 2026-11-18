# Velero

Velero backs up a whole namespace in one `Backup`, with the PVC data moved to S3. bret restores
the PVCs of that `Backup` into a scratch namespace and runs the check against them.

Tested with Velero 1.18 (Helm chart 12.2.5) and the AWS plugin on kind with Kubernetes 1.37.

Install:

```bash
velero install \
  --provider aws \
  --plugins velero/velero-plugin-for-aws:v1.14.4 \
  --bucket velero \
  --secret-file ./velero-credentials \
  --backup-location-config "region=us-east-1,s3ForcePathStyle=true,s3Url=https://s3.example.internal" \
  --use-node-agent \
  --use-volume-snapshots=false \
  --features=EnableCSI \
  --wait
```

`velero-credentials` is an AWS-style credentials file. Do not commit it:

```ini
[default]
aws_access_key_id = <access key>
aws_secret_access_key = <secret key>
```

Enable the provider in the bret chart with `providers.velero.enabled: true`. The Velero install
namespace is `providers.velero.namespace`, default `velero`. The chart passes it to the operator
as `VELERO_NAMESPACE`.

## 1. Deploy WordPress + MariaDB

Velero needs nothing in the app namespace. Apply the example. It deploys WordPress, MariaDB, the
seed Job and the check.

```bash
kubectl create namespace wordpress
kubectl apply -k examples/velero -n wordpress
kubectl -n wordpress wait --for=condition=Complete job/seed --timeout=15m
```

The app itself is in [examples/wordpress/](../../examples/wordpress/). Its PVCs carry the label
`app.kubernetes.io/name: wordpress`.

## 2. Back up

```bash
velero backup create wordpress-1 \
  --include-namespaces wordpress \
  --snapshot-move-data \
  --labels velero.io/schedule-name=wordpress \
  --wait
```

Velero takes a CSI snapshot of each PVC, then the node agent moves the data to S3.

**Required:** `--snapshot-move-data`. Without it, the volume data stays in the cluster as a
snapshot. A restore then works only while that snapshot exists, and off-cluster recovery does
not work.

For regular backups use a Velero `Schedule` named `wordpress` with `snapshotMoveData: true`.
Velero sets `velero.io/schedule-name: wordpress` on every `Backup` it creates, which is what the
`RestoreTest` below selects. The `--labels` flag above sets the same label on a manual backup.

## 3. Write the check

The check is the same for every tool:
[checks/wordpress/restorecheck.yaml](../../checks/wordpress/restorecheck.yaml). With Velero
the check pod runs in the scratch namespace. bret copies the Secrets and ConfigMaps the check
names in `imagePullSecrets`, `env` and `envFrom` (here `wordpress-db`) into it first.

A check for Velero must not set any of these; such a check fails with `ProviderUnsupported`:

- `serviceAccountName`
- a configMap, secret, projected or persistentVolumeClaim volume
- a hostPath volume

Pull secrets of the `default` ServiceAccount are not copied. List the pull secret in the check's
`imagePullSecrets`. How to write a check: [checks.md](../checks.md).

## 4. Restore by hand, then a RestoreTest

A `Restore` lists every PVC as a source. All sources name the same Velero `Backup`:
[examples/velero/restore.yaml](../../examples/velero/restore.yaml).

```yaml
# Restore: one manual restore test of the Velero Backup wordpress-1.
# One Velero Backup holds every PVC of the namespace, so all sources name the same backupRef.
# bret restores only PVCs and PVs, into a scratch namespace bret-scratch-<hash>, under their
# source names. Velero picks the size and storage class, so sources have no size fields.
apiVersion: bret.linogics.io/v1alpha1
kind: Restore
metadata:
  name: wordpress-manual
spec:
  provider: velero
  sources:
    - sourcePVC: html
      backupRef:
        name: wordpress-1   # A Velero Backup in the Velero namespace.
    - sourcePVC: mysql
      backupRef:
        name: wordpress-1
  velero:                   # Optional. Restore only the PVCs with these labels.
    labelSelector:
      matchLabels:
        app.kubernetes.io/name: wordpress
  restoreCheckRef:
    name: wordpress
```

```bash
kubectl apply -f examples/velero/restore.yaml -n wordpress
kubectl -n wordpress wait --for=jsonpath='{.status.phase}'=Succeeded restore/wordpress-manual --timeout=15m
```

`spec.velero` is optional. It narrows what Velero restores, with one of two selectors, which are
mutually exclusive:

- `labelSelector`: a standard label selector. One selector matches all the PVCs.
- `orLabelSelectors`: a list of selectors. A PVC matches if any one matches. Use it when the PVCs
  carry different labels.

A selector matches the labels stored in the backup, so a label added after the backup does not
count. Every `sourcePVC` must be among the restored PVCs; otherwise the `Restore` ends `Failed`
with `RestoredVolumeMissing`, naming the PVC.

The backup must hold the volume data as a snapshot: CSI snapshots, best with
`--snapshot-move-data`, or native volume snapshots. Velero writes file system backup data only
while it restores the pod that mounted the volume, and bret restores no pods. A source PVC saved
with file system backup, or a `Backup` with `snapshotVolumes: false`, ends the `Restore` as
`Failed` with `BackupUnsupported` before Velero starts.

To see a failure, apply [examples/velero/negative/restore.yaml](../../examples/velero/negative/restore.yaml).
It lists only `html`. It ends `Failed` with reason `SourceMissing`.

A `RestoreTest` picks the newest `Backup` and repeats the test:
[examples/velero/restoretest.yaml](../../examples/velero/restoretest.yaml).

```yaml
# RestoreTest: restore the newest Completed Velero Backup that covers this namespace.
# Every source has the same backupSelector, because one Backup holds both PVCs.
apiVersion: bret.linogics.io/v1alpha1
kind: RestoreTest
metadata:
  name: wordpress
spec:
  provider: velero
  sources:
    - sourcePVC: html
      backupSelector:
        labels:
          velero.io/schedule-name: wordpress
    - sourcePVC: mysql
      backupSelector:
        labels:
          velero.io/schedule-name: wordpress
  velero:
    labelSelector:
      matchLabels:
        app.kubernetes.io/name: wordpress
  restoreCheckRef:
    name: wordpress
  triggers:
    cron: "17 3 * * *"
    afterBackup:
      delay: 30m
    manual: true
  restoreWindow: 45m
  retries:
    count: 1
    backoff: 10m
```

```bash
kubectl apply -f examples/velero/restoretest.yaml -n wordpress
kubectl -n wordpress annotate --overwrite restoretest/wordpress bret.linogics.io/run-now="$(date +%s)"
kubectl -n wordpress get restoretest wordpress
```

All fields: [restoretest-guide.md](../restoretest-guide.md).

## 5. How bret picks backups

- **Object:** Velero `Backup` in the Velero namespace (`VELERO_NAMESPACE`, default `velero`).
- **`backupSelector.name`:** that `Backup`. **`labels`:** every `Backup` with these labels, for
  example `velero.io/schedule-name: <schedule>`.
- **One Backup for all sources:** every source must have the same `backupSelector`, and every
  `Restore` source the same `backupRef`. The API rejects anything else.
- **Counts when:** `status.phase` is `Completed`. `PartiallyFailed`, `Failed` and `InProgress`
  do not count.
- **Namespace coverage:** a `Backup` counts only when it covers the `RestoreTest` namespace:
  `spec.includedNamespaces` is empty, contains `*`, or contains the namespace, and
  `spec.excludedNamespaces` does not contain it. A run never tests a `Backup` without your data.
- **Completion time:** `status.completionTimestamp`. The newest wins; equal times are ordered by
  name.
- **afterBackup:** a run starts when a newer `Backup` than the one of the last run
  (`status.lastHandledBackups`) exists. All entries name the same `Backup`. A `Backup` that
  completed before the `RestoreTest` was created never starts a run.

What bret creates. Here the `Restore` is in the namespace `wordpress`:

```text
┌ velero ───────────────┐   ┌ wordpress (your ns) ───┐
│ Backup      (read)    │   │ RestoreTest            │
│ Restore     (created) │◄──│ Restore                │
└──────────┬────────────┘   │ RestoreCheck           │
           │                └───────────┬────────────┘
           ▼                            │ copies pod-security labels
┌ bret-scratch-<hash> ──────────────────▼────────────┐
│ restored PVC + PV   (only those of "wordpress")    │
│ check pod           + copied Secrets, ConfigMaps   │
└────────────────────────────────────────────────────┘
  hash = first 12 hex of sha256("<namespace>/<name>")
```

- The Velero `Restore` restores only PVCs and PVs (`includedResources`), only of the `Restore`
  namespace (`includedNamespaces`), into `bret-scratch-<hash>` (`namespaceMapping`). Deployments
  of the backup are not restored, so nothing else mounts the restored volumes.
- PVCs keep their source names in the scratch namespace. Velero restores at the original size and
  storage class, so a Velero source has no `size`, `autoExpand` or `storageClassName`.
- bret reads the Velero `Restore` `status.phase`. `Completed` runs the check. `Failed`,
  `PartiallyFailed` and `FailedValidation` fail the attempt.
- The operator copies the `pod-security.kubernetes.io/*` labels to the scratch namespace before
  the check pod starts.

## 6. Troubleshooting

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
