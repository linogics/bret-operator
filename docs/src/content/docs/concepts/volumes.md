---
title: Volumes and scratch space
description: Which PVCs bret restores, where they go and how big the scratch volume is.
---

## Which PVC gets restored

An app often has more than one PVC. The default example here is nextcloud with two: `html`
(files) and `pgdata` (database). A `RestoreCheck` lists both in `restoredVolumes`. A `Restore`
(or `RestoreTest`) has one source for each. `sourcePVC` is the key that ties them together.

```d2 title="RestoreCheck volumes and Restore sources matched by sourcePVC"
check: "RestoreCheck.restoredVolumes[]" {
  style.font-size: 24
  top: 0
  left: 0
  html: "html ->\n/var/www/html" {
    top: 0
    left: 0
    width: 320
    height: 100
  }
  pgdata: "pgdata ->\n/var/lib/postgresql/data" {
    top: 140
    left: 0
    width: 320
    height: 100
  }
}
restore: "Restore.sources[]" {
  style.font-size: 24
  top: 0
  left: 640
  html: "html\n(backupRef: html-1)" {
    top: 0
    left: 0
    width: 260
    height: 100
  }
  pgdata: "pgdata\n(backupRef: pgdata-1)" {
    top: 140
    left: 0
    width: 260
    height: 100
  }
}
check.html <-> restore.html: sourcePVC
check.pgdata <-> restore.pgdata: sourcePVC
```

| Provider | What a source restores | Restored claim | Namespace |
|---|---|---|---|
| `kopiur` | The PVC in one `Snapshot`. One `Snapshot` holds one PVC. | `<restore>-<sourcePVC>-<hash>` | The `Restore` namespace. |
| `volsync` | The PVC of one `ReplicationSource`. | `<restore>-<sourcePVC>-<hash>` | The `Restore` namespace. |
| `velero` | Its `sourcePVC` from the one `Backup` that all sources name. `spec.velero` can narrow the restore with label selectors. | The source PVC name, for example `html` and `pgdata`. | `bret-scratch-<hash>`. |

- A Kopiur or VolSync claim name is `<restore>-<sourcePVC>-<hash>`. The hash of the pair keeps
  the names of two `Restore` objects apart, for example `db` with `data-x` and `db-data` with `x`.
  When the name is longer than 63 characters, the operator shortens it.
- Velero restores all PVCs of the `Restore` namespace from the `Backup`. The optional selectors
  in `spec.velero` match the labels stored in the backup. `labelSelector` and `orLabelSelectors`
  are exclusive.
- One restore reads all sources. The check pod mounts every restored claim at the `mountPath` of
  its `sourcePVC`.

The operator checks that the pieces fit. Each one ends the `Restore` as `Failed`: the `Ready`
condition is `False` with that reason, a Warning Event is written, and no check pod runs. A
`RestoreTest` retries it like any failed attempt, until `retries` run out:

- `SourceMissing`: the `sourcePVC` set of the check and of the `Restore` differ. A volume of the
  check has no source, or a source has no volume.
- `SourceMismatch` (Kopiur and VolSync): the backup holds another PVC than the `sourcePVC` of its
  source. For example, `html` points at the `Snapshot` of `pgdata`.
- `RestoredVolumeMissing` (Velero): a `sourcePVC` of the `Restore` was not restored. The selector
  did not match it, or the backup does not contain it.
- `CheckRefMissing` (Velero): a Secret or ConfigMap that the check needs is not in the `Restore`
  namespace, so the operator cannot copy it into the scratch namespace.
- `BackupUnsupported` (Velero): the backup holds no volume data that a PVC restore brings back.
  The PVC was saved with file system backup (Velero writes that data only when it restores the
  pod), or the `Backup` has `snapshotVolumes: false`. The restore would give an empty volume.
- `BackupNotFound` (VolSync): the mover found no snapshot at or before `restoreAsOf`, so the
  restored volume is empty.

A one-volume app has one entry on each side. It works the same way.

## The scratch-volume rule

The operator creates a volume for each restore attempt. The `Restore` owns the volume (except for Velero), because Velero creates the PVC itself in the scratch namespace.
A restore never writes into a live application volume.

The check pod mounts the scratch volume. It reads the restored data. It must not change
the application's own volume.

## Scratch volume size

The diagram shows how a `Restore` picks the size of its scratch volume, and which reason it sets
when it cannot.

```d2 title="How a Restore picks the scratch volume size"
set: "size set?" {
  shape: diamond
  top: 0
  left: 0
  width: 400
  height: 120
}
known: "source size\nknown?" {
  shape: diamond
  top: 180
  left: 0
  width: 400
  height: 140
}
unknown: SizeUnknown {
  top: 210
  left: 560
  width: 240
  height: 80
}
auto: "autoExpand\n(default: percentage 10,\nmin 1Gi)\nclamp(source x\n(100 + percentage) / 100,\nmin, max)" {
  top: 380
  left: 0
  width: 400
  height: 160
}
cap: "source size\nknown, and size\nbelow it?" {
  shape: diamond
  top: 600
  left: 0
  width: 400
  height: 220
}
insufficient: InsufficientCapacity {
  top: 680
  left: 560
  width: 240
  height: 80
}
resolved: "status.resolvedSize\n(resolved once)" {
  top: 880
  left: 0
  width: 400
  height: 100
}
set -> cap: "yes, use it"
set -> known: "no"
known -> unknown: "no"
known -> auto: "yes"
auto -> cap: size
cap -> insufficient: "yes"
cap -> resolved: "no"
```

`size` is optional on every source (Kopiur and VolSync). The size is resolved for each source. With
an unknown source size, a set `size` is used without the capacity check.

Two more failures are reported as reasons on the `Restore`. Both end it as `Failed`, and nothing is
created:

- `SizeUnknown`: the source size is unknown and `size` is not set. It is also set for a
  running `Restore` from an older operator version that has no `status.resolvedSize`.
- `InsufficientCapacity`: `size`, or the `autoExpand` result, is smaller than the source
  size.

The source size comes from the provider:

- Kopiur: the data size of the `Snapshot`.
- Velero: the PVC request in the backup.
- VolSync: the request of the source PVC.

`autoExpand` has these fields:

- `percentage`: headroom on top of the source. Above 0, at most 1000.
- `min` and `max`: optional bounds. `min` must not exceed `max`.

Example: a 20Gi source with the default gives 22Gi. For a fixed size, set `size`. Or use
`percentage: 100, min: X, max: X` with X at least the source size.
