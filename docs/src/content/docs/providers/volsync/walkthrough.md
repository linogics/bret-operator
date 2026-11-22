---
title: VolSync walkthrough
description: WordPress and MariaDB backed up with VolSync, restored and checked by bret.
---

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

The app itself is in [examples/wordpress/](https://github.com/linogics/bret-operator/tree/main/examples/wordpress).

## 2. Back up

[examples/volsync/backup.yaml](https://github.com/linogics/bret-operator/blob/main/examples/volsync/backup.yaml): one `ReplicationSource` per
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
[checks/wordpress/restorecheck.yaml](https://github.com/linogics/bret-operator/blob/main/checks/wordpress/restorecheck.yaml). The annotation
`bret.linogics.io/mover-uid` gives the restore mover of each volume its UID. Without it the mover
uses the first container's `runAsUser`, then 65534. How to write a check: [Write a check](/guides/write-a-check/).

## 4. Restore by hand, then a RestoreTest

A `Restore` names one `ReplicationSource` per source:
[examples/volsync/restore.yaml](https://github.com/linogics/bret-operator/blob/main/examples/volsync/restore.yaml). Without `restoreAsOf` it
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

To see a failure, apply [examples/volsync/negative/restore.yaml](https://github.com/linogics/bret-operator/blob/main/examples/volsync/negative/restore.yaml).
It restores only `html`. It ends `Failed` with reason `SourceMissing`.

A `RestoreTest` picks the last sync of each source and repeats the test:
[examples/volsync/restoretest.yaml](https://github.com/linogics/bret-operator/blob/main/examples/volsync/restoretest.yaml).

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

All fields: [Schedule restore tests](/guides/restore-tests/).

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

```d2 title="Objects bret reads and creates for a VolSync restore"
restore: my-restore-namespace {
  style.font-size: 24
  top: 0
  left: 60
  secret: "repository Secret\n(must be here)" {
    top: 0
    left: 0
    width: 220
    height: 100
  }
  rd: ReplicationDestination {
    top: 0
    left: 260
    width: 260
    height: 100
  }
  pvc: scratch PVC {
    top: 0
    left: 680
    width: 160
    height: 100
  }
  pod: check pod {
    top: 0
    left: 960
    width: 140
    height: 100
  }
  rd -> pvc: "restores\ninto"
  pod -> pvc: mounts
}
backup: "the Restore namespace" {
  style.font-size: 24
  top: 280
  left: 0
  rs: ReplicationSource {
    style.font-size: 24
    top: 0
    left: 0
    repo: restic.repository {
      top: 0
      left: 0
      width: 220
      height: 80
    }
    src: sourcePVC {
      top: 0
      left: 260
      width: 260
      height: 80
    }
  }
}
backup.rs.repo -- restore.secret: {
  style.stroke-dash: 4
  source-arrowhead.label: references
}
backup.rs.src -> restore.rd: restored by
```

Mover UID: the annotation `bret.linogics.io/mover-uid`, else `runAsUser` of the first check
container, else `65534`.

- The `ReplicationDestination` uses a restic mover and the repository Secret of the
  `ReplicationSource`. The Secret must be in the `Restore` namespace. Otherwise the `Restore`
  fails with `RepositoryMissing`.
- If `spec.sourcePVC` of the `ReplicationSource` is not the `sourcePVC` of the source, the
  `Restore` fails with `SourceMismatch`.
- The check runs when the `ReplicationDestination` reports `status.latestImage.kind:
  PersistentVolumeClaim`.
