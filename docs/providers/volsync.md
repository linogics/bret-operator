# VolSync

VolSync backs up one PVC per `ReplicationSource` with restic. bret treats the last sync of a
`ReplicationSource` as its backup, restores it into a scratch PVC with a `ReplicationDestination`,
and runs the check against all restored PVCs.

Tested with VolSync 0.17.0 and restic on kind with Kubernetes 1.37.

Install:

```bash
helm repo add backube https://backube.github.io/helm-charts/
helm install volsync backube/volsync --namespace volsync-system --create-namespace
```

Enable the provider in the bret chart with `providers.volsync.enabled: true`.

## 1. Deploy WordPress + MariaDB

Each PVC gets its own restic repository, so create one Secret per PVC. Then apply the example. It
deploys WordPress, MariaDB, the seed Job, the check and the two `ReplicationSources`.

```bash
kubectl create namespace wordpress
for pvc in html mysql; do
  kubectl -n wordpress create secret generic "restic-$pvc" \
    --from-literal=RESTIC_REPOSITORY="s3:https://s3.example.internal/volsync/wordpress/$pvc" \
    --from-literal=RESTIC_PASSWORD=<a strong password you keep safe> \
    --from-literal=AWS_ACCESS_KEY_ID=<access key> \
    --from-literal=AWS_SECRET_ACCESS_KEY=<secret key>
done
kubectl apply -k examples/volsync -n wordpress
kubectl -n wordpress wait --for=condition=Complete job/seed --timeout=15m
```

The app itself is in [examples/wordpress/](../../examples/wordpress/).

## 2. Back up

[examples/volsync/backup.yaml](../../examples/volsync/backup.yaml): one `ReplicationSource` per
PVC. A sync takes a CSI snapshot of the PVC (`copyMethod: Snapshot`), then copies it to the
restic repository in S3.

```yaml
# VolSync backup: one ReplicationSource per PVC, each with its own restic repository in S3.
# A shared repository races on init, so each PVC has its own Secret (restic-html, restic-mysql)
# with RESTIC_REPOSITORY, RESTIC_PASSWORD, AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY.
# Each sync takes a CSI snapshot, then copies it with restic. The schedule runs nightly. For one
# backup now, patch spec.trigger to {manual: <id>}. The snapshot class is the kind dev name.
apiVersion: volsync.backube/v1alpha1
kind: ReplicationSource
metadata:
  name: html
spec:
  sourcePVC: html
  trigger:
    schedule: "0 2 * * *"
  restic:
    repository: restic-html
    copyMethod: Snapshot
    volumeSnapshotClassName: csi-hostpath-snapclass
    retain:
      daily: 7
    moverSecurityContext:
      runAsUser: 33
      runAsGroup: 33
      fsGroup: 33
---
apiVersion: volsync.backube/v1alpha1
kind: ReplicationSource
metadata:
  name: mysql
spec:
  sourcePVC: mysql
  trigger:
    schedule: "0 2 * * *"
  restic:
    repository: restic-mysql
    copyMethod: Snapshot
    volumeSnapshotClassName: csi-hostpath-snapclass
    retain:
      daily: 7
    moverSecurityContext:
      runAsUser: 999
      runAsGroup: 999
      fsGroup: 999
```

The schedule runs a sync every night. To back up now, after the seed Job is done, replace the
schedule with a manual trigger and wait for it:

```bash
kubectl -n wordpress patch replicationsource html mysql --type=merge \
  -p '{"spec":{"trigger":{"schedule":null,"manual":"wordpress-1"}}}'
kubectl -n wordpress wait --for=jsonpath='{.status.lastManualSync}'=wordpress-1 replicationsource --all --timeout=15m
```

Do not apply a manual trigger together with the app. VolSync syncs at once, before MariaDB has
created its tables, and the backup holds an empty database. Set the schedule back afterwards.

Set `moverSecurityContext` to the owner of the data: 33 for the WordPress docroot, 999 for
MariaDB. The default mover UID cannot read a database data dir (`permission denied`).

## 3. Write the check

The check is the same for every tool:
[checks/wordpress/restorecheck.yaml](../../checks/wordpress/restorecheck.yaml). The annotation
`bret.linogics.io/mover-uid` gives the restore mover of each volume its UID; without it the mover
uses the first container's `runAsUser`, then 65534. How to write a check: [checks.md](../checks.md).

## 4. Restore by hand, then a RestoreTest

A `Restore` names one `ReplicationSource` per source:
[examples/volsync/restore.yaml](../../examples/volsync/restore.yaml). Without `restoreAsOf` it
restores the newest restic snapshot.

```yaml
# Restore: one manual restore test of the newest restic snapshot of each ReplicationSource.
# backupRef names the ReplicationSource. restoreAsOf (optional) pins an older snapshot.
# Restored claims: <restore>-<sourcePVC>-<hash>, here wordpress-manual-html-<hash> and wordpress-manual-mysql-<hash>.
apiVersion: bret.linogics.io/v1alpha1
kind: Restore
metadata:
  name: wordpress-manual
spec:
  provider: volsync
  sources:
    - sourcePVC: html
      backupRef:
        name: html                       # The ReplicationSource of the html PVC.
      storageClassName: csi-hostpath-sc
    - sourcePVC: mysql
      backupRef:
        name: mysql
      storageClassName: csi-hostpath-sc
  restoreCheckRef:
    name: wordpress
```

```bash
kubectl apply -f examples/volsync/restore.yaml -n wordpress
kubectl -n wordpress wait --for=jsonpath='{.status.phase}'=Succeeded restore/wordpress-manual --timeout=15m
```

To see a failure, apply [examples/volsync/negative/restore.yaml](../../examples/volsync/negative/restore.yaml).
It restores only `html`. It ends `Failed` with reason `SourceMissing`.

A `RestoreTest` picks the last sync of each source and repeats the test:
[examples/volsync/restoretest.yaml](../../examples/volsync/restoretest.yaml).

```yaml
# RestoreTest: restore each PVC from its ReplicationSource and run the check.
# The backup is the last sync (status.lastSyncTime). bret pins the restore to that time,
# so a sync during the run does not change what is tested.
apiVersion: bret.linogics.io/v1alpha1
kind: RestoreTest
metadata:
  name: wordpress
spec:
  provider: volsync
  sources:
    - sourcePVC: html
      backupSelector:
        name: html       # The ReplicationSource. labels must match exactly one.
      storageClassName: csi-hostpath-sc
    - sourcePVC: mysql
      backupSelector:
        name: mysql
      storageClassName: csi-hostpath-sc
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
kubectl apply -f examples/volsync/restoretest.yaml -n wordpress
kubectl -n wordpress annotate --overwrite restoretest/wordpress bret.linogics.io/run-now="$(date +%s)"
kubectl -n wordpress get restoretest wordpress
```

All fields: [restoretest-guide.md](../restoretest-guide.md).

## 5. How bret picks backups

VolSync has no object per backup. The backup is the last sync of a `ReplicationSource`.

- **Object:** `ReplicationSource` in the `RestoreTest` namespace.
- **`backupSelector.name`:** that `ReplicationSource`. **`labels`:** the `ReplicationSource` with
  these labels. VolSync keeps one `ReplicationSource` per PVC, so labels must match exactly one.
  More than one blocks the test with reason `BackupSelectorAmbiguous`, naming the matches.
- **Counts when:** `status.lastSyncTime` is set. A source that never synced blocks with
  `BackupNotFound`.
- **Completion time:** `status.lastSyncTime`.
- **afterBackup:** a run starts when every source has a newer `lastSyncTime` than the one of the
  last run (`status.lastHandledBackups`). A sync that finished before the `RestoreTest` was
  created never starts a run, so a new test waits for the next sync.
- **Pinning:** each `Restore` source gets `restoreAsOf` = the picked `lastSyncTime`. The
  `ReplicationDestination` then restores the newest restic snapshot at or before that time
  (`spec.restic.restoreAsOf`). A sync that lands during the run, or while a run waits in the
  queue or for a retry, does not change what is tested.

What bret creates:

```text
┌ backup namespace ─────────┐      ┌ Restore namespace ───────────┐
│ ReplicationSource         │      │ repository Secret (must be   │
│   restic.repository ──────┼─────►│   here)                      │
│   sourcePVC ──────────────┼─────►│ ReplicationDestination       │
└───────────────────────────┘      │ scratch PVC ─► check pod     │
 backupRef.namespace,              └──────────────────────────────┘
 else the Restore namespace

mover UID:  annotation bret.linogics.io/mover-uid ─► first container runAsUser ─► 65534
```

- The `ReplicationDestination` uses a restic mover and the repository Secret of the
  `ReplicationSource`. The Secret must be in the `Restore` namespace; otherwise the `Restore`
  fails with `RepositoryMissing`.
- If `spec.sourcePVC` of the `ReplicationSource` is not the `sourcePVC` of the source, the
  `Restore` fails with `SourceMismatch`.
- The check runs when the `ReplicationDestination` reports `status.latestImage.kind:
  PersistentVolumeClaim`.

## 6. Troubleshooting

### VolSync mover: `permission denied` on the data directory

- **Cause:** the mover runs as the default non-database UID.
- **Fix:** set `moverSecurityContext` in the `ReplicationSource` to the owner UID, and the
  `bret.linogics.io/mover-uid` annotation on the check for the restore.

### VolSync: `permission denied` after changing the mover UID

- **Cause:** the cache PVC was created by the old mover UID.
- **Fix:** delete the cache PVC. VolSync creates a new one.

### RestoreTest stops at `BackupSelectorAmbiguous`

- **Cause:** the `labels` of a source match more than one `ReplicationSource`.
- **Fix:** use `backupSelector.name`, or labels that only one `ReplicationSource` carries.

### Restore fails with `RepositoryMissing`

- **Cause:** the `ReplicationSource` has no `spec.restic.repository`, or its Secret is not in the
  `Restore` namespace.
- **Fix:** keep the repository Secret next to the `Restore`.
