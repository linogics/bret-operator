---
title: Add bret to an existing Kopiur setup
description: Checklist for a cluster that already backs up PVCs with Kopiur.
---

Your app's PVCs already have Kopiur `SnapshotPolicy` objects. Work through the list in order.

## 1. One SnapshotPolicy per PVC

- One `Snapshot` holds one PVC. Give each PVC its own `SnapshotPolicy` with one `pvc:` source.
- Several policies can share one `Repository`.
- Kopiur labels each `Snapshot` with `kopiur.home-operations.com/config: <policy>`. With one
  policy per PVC, that label selects the `Snapshots` of exactly one PVC. It is the usual label
  for `backupSelector.labels`.

```bash
kubectl -n shop get snapshot --show-labels
```

## 2. The credentials Secret must be in the Restore namespace

A Kopiur `Restore` runs in the `Restore` namespace and reads the repository credentials Secret
there. Kopiur does not find the Secret in another namespace, and the restore fails with
`MissingCredentialsSecret`. Keep the Secret that the `Repository` names next to the app.

## 3. Set the mover UID

The Kopia mover runs as UID 65532 by default. A database data dir with mode 0700 is unreadable
for it.

- On the `SnapshotPolicy`, set `spec.mover.securityContext.runAsUser` to the owner of the data,
  for example `999` for MariaDB or PostgreSQL. Otherwise the snapshot fails with
  `PermissionDenied`.
- On the restore side, bret picks the mover UID per source in this order:
  1. the check annotation `bret.linogics.io/mover-uid`, a JSON map keyed by `sourcePVC`,
  2. `runAsUser` of the first check container that sets one,
  3. none, so Kopiur uses its default.

```yaml
metadata:
  annotations:
    bret.linogics.io/mover-uid: '{"data": 999}'
```

## 4. Know which Snapshot bret picks

- **Object:** Kopiur `Snapshot` in the `RestoreTest` namespace.
- **`backupSelector.name`** picks one `Snapshot`. **`labels`** picks every `Snapshot` with those
  labels.
- **Counts when:** `status.phase` is `Succeeded`.
- **Newest:** `lastTransitionTime` of the `Ready=True` condition.
- **afterBackup** starts a run when every source has a newer `Snapshot` than the last run.

## 5. Enable the provider in the bret chart

Kopiur is on by default. To be explicit:

```yaml
providers:
  kopiur:
    enabled: true
```

The chart grants `get`, `list`, `watch` on `snapshots`, `get` on `snapshotpolicies`, and full
rights on `restores` in `kopiur.home-operations.com`.

## 6. Write a check and a RestoreTest

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
  provider: kopiur
  sources:
    - sourcePVC: data
      backupSelector:
        labels:
          kopiur.home-operations.com/config: data
  restoreCheckRef:
    name: shop
  triggers:
    afterBackup:
      delay: 30m
    manual: true
```

## 7. Run it once and read the result

A `Snapshot` older than the `RestoreTest` never starts an `afterBackup` run. Start the first run
by hand:

```bash
kubectl -n shop annotate --overwrite restoretest/shop bret.linogics.io/run-now="$(date +%s)"
kubectl -n shop get restore -w
kubectl -n shop describe restore <name>
```

`Succeeded` means the check exited 0. For `Failed`, look up the condition reason in
[Failure reasons](/reference/failure-reasons/) and
[Kopiur limitations](/providers/kopiur/limitations/).
