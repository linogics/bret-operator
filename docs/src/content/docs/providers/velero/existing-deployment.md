---
title: Add bret to an existing Velero setup
description: Checklist for a cluster that already runs Velero, from backup settings to the first result.
---

Your cluster runs Velero and backs up apps on a `Schedule`. Work through the list in order.

## 1. Volume data must be a snapshot

bret restores only PVCs and PVs, no pods. That decides which Velero backup modes work:

| How the PVC was backed up | Works with bret | Why |
|---|---|---|
| CSI snapshot with data movement (`snapshotMoveData: true`) | Yes, recommended | Data is in S3 and survives the loss of the cluster. |
| CSI snapshot without data movement | Yes, while the snapshot exists | The data stays in the cluster as a `VolumeSnapshot`. |
| Native volume snapshot (cloud provider plugin) | Yes | Restored with the PV. |
| File system backup (`backup.velero.io/backup-volumes` on the pod) | No | Velero writes file system backup data only while it restores the pod that mounted the volume. |
| File system backup by default (`defaultVolumesToFsBackup: true`) | No, unless the PVC is excluded | Same reason. |
| `snapshotVolumes: false` | No | The `Backup` holds no volume data. |

bret checks this before Velero starts. A `Backup` with `snapshotVolumes: false`, or a source PVC
with a `PodVolumeBackup` in the `Backup`, ends the `Restore` `Failed` with reason
[`BackupUnsupported`](/reference/failure-reasons/).

If your `Schedule` uses file system backup:

- With opt-in (the pod annotation `backup.velero.io/backup-volumes: <volume names>`), remove the
  app's volumes from that annotation.
- With opt-out (`defaultVolumesToFsBackup: true`), list the app's volumes in the pod annotation
  `backup.velero.io/backup-volumes-excludes: <volume names>`.

Velero then snapshots those volumes with CSI. Set `snapshotMoveData: true` on the `Schedule` so
the data reaches S3. A `Schedule` that does this:

```yaml
apiVersion: velero.io/v1
kind: Schedule
metadata:
  name: shop
  namespace: velero
spec:
  schedule: "0 2 * * *"
  template:
    includedNamespaces:
      - shop
    snapshotMoveData: true
    ttl: 168h0m0s
```

The CSI driver of the PVC needs a `VolumeSnapshotClass`. See
[Shared setup](/providers/shared-setup/#before-any-tool-csi-snapshots).

## 2. Know which Backup bret picks

A `RestoreTest` picks the newest `Backup` that:

- is in the Velero namespace,
- has `status.phase: Completed` (`PartiallyFailed` does not count),
- matches `backupSelector`: `name` picks one `Backup`, `labels` picks every `Backup` with those
  labels, for example `velero.io/schedule-name: shop`,
- covers the `RestoreTest` namespace: `spec.includedNamespaces` is empty, or one of its glob
  patterns matches, and no `spec.excludedNamespaces` pattern matches.

Newest means the latest `status.completionTimestamp`. Check what bret will see:

```bash
kubectl -n velero get backup -l velero.io/schedule-name=shop \
  -o custom-columns=NAME:.metadata.name,PHASE:.status.phase,DONE:.status.completionTimestamp
```

## 3. Enable the provider in the bret chart

```yaml
providers:
  velero:
    enabled: true
    namespace: velero
```

`providers.velero.namespace` is the Velero install namespace, default `velero`. The chart passes
it to the operator as `VELERO_NAMESPACE`.

With Velero on, the chart grants:

| API group | Resources | Verbs | Why |
|---|---|---|---|
| `velero.io` | `backups`, `podvolumebackups` | get, list, watch | Pick backups, detect file system backup. |
| `velero.io` | `restores` | create, delete, get, list, patch, watch | Drive the Velero `Restore`. |
| core | `namespaces` | get, patch, delete | Label and delete the scratch namespace. |
| core | `secrets`, `configmaps` | get, create | Copy what the check names into the scratch namespace. |
| core | `pods`, `persistentvolumeclaims` | create, delete, get, list, patch, watch | Cluster-wide, because the scratch namespace is not watched. |

With `watchNamespaces` set, the `velero.io` rights are a `Role` in the Velero namespace. Details:
[Scope](/concepts/overview/#scope).

## 4. Write a check for the app

A check is a pod template. Exit code 0 means the restored data is good. Mount each PVC under
`restoredVolumes`. A minimal check that tests that a file exists and is not empty:

```yaml
apiVersion: bret.linogics.io/v1alpha1
kind: RestoreCheck
metadata:
  name: shop
  namespace: shop
spec:
  restoredVolumes:
    - sourcePVC: data
      mountPath: /data
  containers:
    - name: check
      image: busybox:1.37
      command: ["sh", "-c", "test -s /data/orders.db"]
```

With Velero, a check must not use `serviceAccountName`, `hostPath`, or a configMap, secret,
projected or persistentVolumeClaim volume. Such a check ends `Failed` with
`ProviderUnsupported`. Pass Secrets with `env` or `envFrom`: bret copies
them into the scratch namespace. More: [Write a check](/guides/write-a-check/) and the
[Check catalog](/guides/check-catalog/).

## 5. Create a RestoreTest

```yaml
apiVersion: bret.linogics.io/v1alpha1
kind: RestoreTest
metadata:
  name: shop
  namespace: shop
spec:
  provider: velero
  sources:
    - sourcePVC: data
      backupSelector:
        labels:
          velero.io/schedule-name: shop
  restoreCheckRef:
    name: shop
  triggers:
    afterBackup:
      delay: 30m
    manual: true
```

Every source names the same `backupSelector`, because one `Backup` holds every PVC of the
namespace.

## 6. Run it once and read the result

A `Backup` that completed before the `RestoreTest` existed never starts an `afterBackup` run.
Start the first run by hand:

```bash
kubectl -n shop annotate --overwrite restoretest/shop bret.linogics.io/run-now="$(date +%s)"
kubectl -n shop get restore -w
```

When the `Restore` ends:

```bash
kubectl -n shop get restoretest shop
kubectl -n shop describe restore <name>
```

`Succeeded` means the check exited 0. For `Failed`, the condition reason names the cause. Look
it up in [Failure reasons](/reference/failure-reasons/) and
[Velero limitations](/providers/velero/limitations/).
