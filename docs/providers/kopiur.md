# Kopiur (alpha)

Kopiur backs up one PVC per `Snapshot` with Kopia. bret restores each `Snapshot` into a scratch
PVC and runs the check against all of them.

Tested with Kopiur 0.11.0 on kind with Kubernetes 1.37. Kopiur is alpha. Upstream issue
`kopiur#233` affects restores and is still open.

Install Kopiur with its Helm chart into `kopiur-system`, as the Kopiur docs describe.

Enable the provider in the bret chart with `providers.kopiur.enabled`. It is on by default.

## 1. Deploy WordPress + MariaDB

Create the namespace and the repository Secret, then apply the example. It deploys WordPress,
MariaDB, the seed Job, the check and the Kopiur backup setup.

```bash
kubectl create namespace wordpress
kubectl -n wordpress create secret generic kopiur-repo-creds \
  --from-literal=AWS_ACCESS_KEY_ID=<access key> \
  --from-literal=AWS_SECRET_ACCESS_KEY=<secret key> \
  --from-literal=KOPIA_PASSWORD=<a strong password you keep safe>
kubectl apply -k examples/kopiur -n wordpress
kubectl -n wordpress wait --for=condition=Complete job/seed --timeout=15m
```

The app itself is in [examples/wordpress/](../../examples/wordpress/).

## 2. Back up

[examples/kopiur/backup.yaml](../../examples/kopiur/backup.yaml): one `Repository` in S3, and
one `SnapshotPolicy` per PVC. A backup takes a CSI snapshot of the PVC (`copyMethod: Snapshot`),
then copies it into the repository.

```yaml
# Kopiur backup: one S3 repository, one SnapshotPolicy per PVC.
# Each backup takes a CSI snapshot of the PVC, then copies it to the repository.
# The Secret kopiur-repo-creds holds AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY and KOPIA_PASSWORD.
# The endpoint and snapshot class are the kind dev cluster names. Replace them.
apiVersion: kopiur.home-operations.com/v1alpha1
kind: Repository
metadata:
  name: primary
spec:
  backend:
    s3:
      bucket: kopiur
      prefix: wordpress/
      endpoint: versitygw.storage.svc.cluster.local:7070
      region: us-east-1
      tls:
        disableTls: true
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
  name: html
spec:
  repository:
    kind: Repository
    name: primary
  sources:
    - pvc:
        name: html
  copyMethod: Snapshot
  volumeSnapshotClassName: csi-hostpath-snapclass
  retention:
    keepDaily: 7
  mover:
    securityContext:
      runAsUser: 33
      runAsGroup: 33
      runAsNonRoot: true
---
apiVersion: kopiur.home-operations.com/v1alpha1
kind: SnapshotPolicy
metadata:
  name: mysql
spec:
  repository:
    kind: Repository
    name: primary
  sources:
    - pvc:
        name: mysql
  copyMethod: Snapshot
  volumeSnapshotClassName: csi-hostpath-snapclass
  retention:
    keepDaily: 7
  mover:
    securityContext:
      runAsUser: 999
      runAsGroup: 999
      runAsNonRoot: true
```

**One `Snapshot` holds one PVC.** A `SnapshotPolicy` with several `pvc:` sources backs up only
the first one; the others are silently skipped. Use one policy per PVC, as above, or one policy
with `pvcSelector`, which makes one `Snapshot` per matching PVC.

The `mover` block is needed for every volume that the default mover UID 65532 cannot read. Set
it to the owner of the data: 33 for the WordPress docroot, 999 for MariaDB.

Take one backup by hand with [examples/kopiur/snapshot.yaml](../../examples/kopiur/snapshot.yaml).
For regular backups, create a `SnapshotSchedule` with a `policyRef` to each policy. Kopiur
labels every `Snapshot`, manual or scheduled, with `kopiur.home-operations.com/config: <policy>`.
The `RestoreTest` below selects on that label, so it finds scheduled backups too.

```yaml
# One manual Kopiur backup per PVC. A SnapshotSchedule creates them on a schedule.
# Kopiur labels every Snapshot with kopiur.home-operations.com/config=<policy>, manual or
# scheduled. The RestoreTest backupSelector matches that label.
apiVersion: kopiur.home-operations.com/v1alpha1
kind: Snapshot
metadata:
  name: html-1
spec:
  policyRef:
    name: html
  deletionPolicy: Retain
---
apiVersion: kopiur.home-operations.com/v1alpha1
kind: Snapshot
metadata:
  name: mysql-1
spec:
  policyRef:
    name: mysql
  deletionPolicy: Retain
```

```bash
kubectl apply -f examples/kopiur/snapshot.yaml -n wordpress
kubectl -n wordpress wait --for=jsonpath='{.status.phase}'=Succeeded snapshot --all --timeout=15m
```

## 3. Write the check

The check is the same for every tool:
[examples/wordpress/restorecheck.yaml](../../examples/wordpress/restorecheck.yaml). It mounts
`html` and `mysql`, starts MariaDB as a sidecar, and compares the canary post with the canary
file. The annotation `bret.linogics.io/mover-uid` gives the restore mover of each volume its UID.
How to write a check: [checks.md](../checks.md).

## 4. Restore by hand, then a RestoreTest

A `Restore` names one `Snapshot` per source:
[examples/kopiur/restore.yaml](../../examples/kopiur/restore.yaml).

```yaml
# Restore: one manual restore test of the Kopiur Snapshots html-1 and mysql-1.
# One source per PVC. The check in restoreCheckRef runs once both PVCs are restored.
# Restored claims: <restore>-<sourcePVC>, here wordpress-manual-html and wordpress-manual-mysql.
apiVersion: bret.linogics.io/v1alpha1
kind: Restore
metadata:
  name: wordpress-manual
spec:
  provider: kopiur
  sources:
    - sourcePVC: html
      backupRef:
        name: html-1                     # A Kopiur Snapshot of the html PVC.
      storageClassName: csi-hostpath-sc  # Storage class for the scratch volume.
    - sourcePVC: mysql
      backupRef:
        name: mysql-1
      storageClassName: csi-hostpath-sc
      autoExpand:                        # Optional. Default: the source size.
        percentage: 25
  restoreCheckRef:
    name: wordpress
```

```bash
kubectl apply -f examples/kopiur/restore.yaml -n wordpress
kubectl -n wordpress wait --for=jsonpath='{.status.phase}'=Succeeded restore/wordpress-manual --timeout=15m
```

To see a failure, apply [examples/kopiur/negative/restore.yaml](../../examples/kopiur/negative/restore.yaml).
It restores only `html`. It ends `Failed` with reason `SourceMissing`.

A `RestoreTest` picks the newest `Snapshot` of each source and repeats the test:
[examples/kopiur/restoretest.yaml](../../examples/kopiur/restoretest.yaml).

```yaml
# RestoreTest: restore the newest Kopiur Snapshot of each PVC and run the check.
# Each backupSelector matches the policy label Kopiur sets on every Snapshot.
apiVersion: bret.linogics.io/v1alpha1
kind: RestoreTest
metadata:
  name: wordpress
spec:
  provider: kopiur
  sources:
    - sourcePVC: html
      backupSelector:
        labels:
          kopiur.home-operations.com/config: html
      storageClassName: csi-hostpath-sc
    - sourcePVC: mysql
      backupSelector:
        labels:
          kopiur.home-operations.com/config: mysql
      storageClassName: csi-hostpath-sc
  restoreCheckRef:
    name: wordpress
  triggers:
    cron: "17 3 * * *"   # 03:17 UTC every day.
    afterBackup:
      delay: 30m         # Also 30 minutes after every source has a new backup.
    manual: true         # On demand with the bret.linogics.io/run-now annotation.
  restoreWindow: 45m
  retries:
    count: 1
    backoff: 10m
```

```bash
kubectl apply -f examples/kopiur/restoretest.yaml -n wordpress
kubectl -n wordpress annotate --overwrite restoretest/wordpress bret.linogics.io/run-now="$(date +%s)"
kubectl -n wordpress get restoretest wordpress
```

All fields: [restoretest-guide.md](../restoretest-guide.md).

## 5. How bret picks backups

- **Object:** Kopiur `Snapshot` in the `RestoreTest` namespace.
- **`backupSelector.name`:** that one `Snapshot`. **`labels`:** every `Snapshot` with these
  labels. Use labels that only the `Snapshots` of one PVC carry, such as
  `kopiur.home-operations.com/config: <policy>` with one policy per PVC.
- **Counts when:** `status.phase` is `Succeeded`.
- **Completion time:** `lastTransitionTime` of the `Ready=True` condition. The newest wins; equal
  times are ordered by name.
- **afterBackup:** a run starts when every source has a `Snapshot` newer than the one of the
  last run (`status.lastHandledBackups`). Snapshots older than the `RestoreTest` do not count.

Each `Restore` source names the `Snapshot` of one PVC. If the `Snapshot` holds another PVC than
the `sourcePVC` of the source, the `Restore` ends Failed with `SourceMismatch`.

### The Kopiur-native restore

bret creates these objects for you, one pair per source. This is what the tool does underneath.

A Kopiur restore runs in a namespace. The repository credentials Secret must be in that
namespace. Kopiur does not find the Secret in another namespace, and the restore fails with
`MissingCredentialsSecret`.

```yaml
apiVersion: kopiur.home-operations.com/v1alpha1
kind: Restore
metadata:
  name: wordpress-mysql-restore
spec:
  source:
    snapshotRef:
      name: mysql-1
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
  name: wordpress-mysql-restored
spec:
  storageClassName: csi-hostpath-sc
  accessModes: ["ReadWriteOnce"]
  resources:
    requests:
      storage: 2Gi
  dataSourceRef:
    apiGroup: kopiur.home-operations.com
    kind: Restore
    name: wordpress-mysql-restore
```

The restore is done when the Kopiur `Restore` has `status.phase: Completed` and its `Ready`
condition has reason `RestoreSucceeded`. Kopiur says `Completed`, not `Succeeded`. Do not confuse
these words with bret's phases.

## 6. Troubleshooting

### Kopiur restore: `MissingCredentialsSecret`

- **Cause:** the restore runs in a namespace without the repository credentials Secret.
- **Fix:** keep the Secret in the namespace of the `Restore`. See
  [The Kopiur-native restore](#the-kopiur-native-restore).

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
