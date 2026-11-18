---
title: Add bret to an existing VolSync setup
description: Checklist for a cluster that already backs up PVCs with VolSync restic.
---

Your app's PVCs already have restic `ReplicationSource` objects. Work through the list in order.

## 1. One ReplicationSource and one repository per PVC

- bret needs one `ReplicationSource` per PVC. That is how VolSync works anyway.
- Give each PVC its own restic repository (its own Secret with its own `RESTIC_REPOSITORY`). A
  repository shared by two `ReplicationSource` objects races on init.
- Only the restic mover is supported. bret reads `spec.restic.repository`. A
  `ReplicationSource` without it fails with [`RepositoryMissing`](/reference/failure-reasons/).

## 2. The repository Secret must be in the Restore namespace

The `ReplicationDestination` that bret creates lives in the `Restore` namespace and uses the
Secret that `spec.restic.repository` names. If the Secret is not there, the `Restore` fails with
`RepositoryMissing`.

A `RestoreTest` reads `ReplicationSource` objects in its own namespace, so the Secret is
already there. A `Restore` made by hand can name a `ReplicationSource` in another namespace with
`backupRef.namespace`. Then copy the Secret into the `Restore` namespace.

## 3. Set the mover UID

The restic mover writes the restored files as one UID. The check must be able to read them, and
the backup mover must have been able to read the source.

- On the `ReplicationSource`, set `moverSecurityContext` to the owner of the data, for example
  `999` for MariaDB or PostgreSQL. With the default UID, the backup fails with
  `permission denied` on a database data dir.
- On the restore side, bret picks the mover UID per source in this order:
  1. the check annotation `bret.linogics.io/mover-uid`, a JSON map keyed by `sourcePVC`,
  2. `runAsUser` of the first check container that sets one,
  3. `65534`.

```yaml
metadata:
  annotations:
    bret.linogics.io/mover-uid: '{"data": 999}'
```

## 4. Know which backup bret picks

- **Object:** the `ReplicationSource` in the `RestoreTest` namespace.
- **`backupSelector.name`** picks one. **`labels`** must match exactly one. More than one blocks
  the test with [`BackupSelectorAmbiguous`](/reference/failure-reasons/), naming the matches.
- **The backup** is the last sync: `status.lastSyncTime`. A source that never synced blocks with
  `BackupNotFound`.
- **afterBackup** starts a run when every source has a newer `lastSyncTime` than the last run.

```bash
kubectl -n shop get replicationsource \
  -o custom-columns=NAME:.metadata.name,PVC:.spec.sourcePVC,REPO:.spec.restic.repository,SYNC:.status.lastSyncTime
```

## 5. Enable the provider in the bret chart

```yaml
providers:
  volsync:
    enabled: true
```

The chart grants `get`, `list`, `watch` on `replicationsources` and `get`, `list`, `watch`,
`create`, `delete`, `patch` on `replicationdestinations` in `volsync.backube`, plus `get` on
Secrets.

## 6. Write a check and a RestoreTest

A minimal check that tests that a file exists and is not empty:

```yaml
apiVersion: bret.linogics.io/v1alpha1
kind: RestoreCheck
metadata:
  name: shop
  namespace: shop
  annotations:
    bret.linogics.io/mover-uid: '{"data": 1000}'
spec:
  restoredVolumes:
    - sourcePVC: data
      mountPath: /data
  containers:
    - name: check
      image: busybox:1.37
      command: ["sh", "-c", "test -s /data/orders.db"]
```

```yaml
apiVersion: bret.linogics.io/v1alpha1
kind: RestoreTest
metadata:
  name: shop
  namespace: shop
spec:
  provider: volsync
  sources:
    - sourcePVC: data
      backupSelector:
        name: data
      autoExpand:
        percentage: 20
  restoreCheckRef:
    name: shop
  triggers:
    afterBackup:
      delay: 30m
    manual: true
```

Without `size`, the scratch PVC gets the size of the source PVC. `autoExpand` adds headroom, here
20 percent. Details:
[Scratch volume size](/concepts/volumes/#scratch-volume-size).

## 7. Run it once and read the result

A sync that finished before the `RestoreTest` existed never starts an `afterBackup` run. Start
the first run by hand:

```bash
kubectl -n shop annotate --overwrite restoretest/shop bret.linogics.io/run-now="$(date +%s)"
kubectl -n shop get restore -w
kubectl -n shop describe restore <name>
```

`Succeeded` means the check exited 0. For `Failed`, look up the condition reason in
[Failure reasons](/reference/failure-reasons/) and
[VolSync limitations](/providers/volsync/limitations/).
