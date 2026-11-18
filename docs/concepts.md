# Concepts

This page explains what bret does and the rules it follows. The schema is in
[reference/api.md](reference/api.md).

## The problem

A backup that you never restored is not proven. bret restores your backups
on a schedule, checks the restored data, and tells you when the check fails.

## The three objects

| Kind | Short name | Purpose | Who writes it |
|---|---|---|---|
| `RestoreCheck` | `rcheck` | The check: a pod template that runs against restored data. Exit code 0 means pass. | You, once per app. |
| `RestoreTest` | `rtest` | The schedule and policy: which backups, which check, when, how many retries. | You. |
| `Restore` | `restore` | One restore attempt run: the native restore object plus the check pod. You create it by hand, or a `RestoreTest` creates it. | You, or the operator. |

A typical setup has one `RestoreCheck` per application. It also has one `RestoreTest` for that
app, with one source per PVC.

## What the operator does, and what it does not do

It does:

- create the native restore object of the chosen tool (Velero `Restore`, VolSync
  `ReplicationDestination`, or Kopiur `Restore`), pointed at a scratch volume,
- wait for that restore to finish,
- create the check pod from the check, with the restored volume mounted,
- take the check pod's exit code as the verdict,
- write the result to `status`, to Events, and to Prometheus metrics,
- clean up pods and volumes, or keep them for debugging when you ask it to.

It does not:

- copy, deduplicate or store backup data,
- install Velero, VolSync or Kopiur, or configure their storage,
- read the check's logs or parse its output. Only the exit code counts.

## Providers

| Provider | Backup the operator reads | Native object created | Restore done when |
|---|---|---|---|
| `kopiur` (alpha) | `Snapshot`, phase `Succeeded`; one PVC each | `kopiur.home-operations.com/v1alpha1` `Restore` | `status.phase` is `Completed` and condition `Ready` is `True` |
| `volsync` | `ReplicationSource`, its `status.lastSyncTime`; one PVC each | `volsync.backube/v1alpha1` `ReplicationDestination` with a manual trigger, pinned with `restoreAsOf` | `status.latestImage.kind` is `PersistentVolumeClaim` |
| `velero` | `Backup` in the Velero namespace, phase `Completed`; every PVC of the namespace | `velero.io/v1` `Restore` into a scratch namespace | `status.phase` is `Completed`, `PartiallyFailed`, `Failed` or `FailedValidation` |

Every source names its `sourcePVC`, for all three. Setup, the namespaces each tool uses, and how
the backups are picked are on the tool pages: [Kopiur](providers/kopiur.md),
[VolSync](providers/volsync.md), [Velero](providers/velero.md).

Enable providers in the Helm chart with `providers.<name>.enabled`. Kopiur is on by
default. Velero and VolSync are off by default. A `Restore` or `RestoreTest` for a disabled
provider stops with reason `ProviderUnsupported`.

## Which PVC gets restored

An app often has more than one PVC. The default example here is nextcloud with two: `html`
(files) and `pgdata` (database). A `RestoreCheck` lists both in `restoredVolumes`. A `Restore`
(or `RestoreTest`) has one source for each. `sourcePVC` is the key that ties them together.

```text
RestoreCheck.restoredVolumes[].sourcePVC ◄──── same set ────► Restore.sources[].sourcePVC
        html    -> /var/www/html                                    html    (backupRef: html-1)
        pgdata  -> /var/lib/postgresql/data                         pgdata  (backupRef: pgdata-1)
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

```text
size set?        ──yes─► use it ───────────────────────────┐
      │ no                                                 │
      ▼                                                    │
source size known? ──no─► SizeUnknown                      │
      │ yes                                                │
      ▼                                                    │
autoExpand (default: percentage 10, min 1Gi)               │
  clamp(source × (100 + percentage) / 100, min, max)       │
      ├◄───────────────────────────────────────────────────┘
      ▼
source size known, and size below it? ──yes─► InsufficientCapacity
      │ no
      ▼
status.resolvedSize   (resolved once)
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

## The restore test lifecycle

The diagram shows the phases of one `Restore`, with the retry and keep rules that apply after it ends.

```text
┌─────────┐
│ Pending │  Restore created. Adds the cleanup finalizer.
└────┬────┘
     ▼
┌─────────┐
│ Running │  Native restore, then check pod.
└────┬────┘
     │
     ├─► check exits 0 ────────────────► Succeeded
     ├─► restore or check fails ───────► Failed
     └─► restoreWindow ends ───────────► Canceled

Terminal phase: cleanup runs after the status is stored.
  Succeeded             deletes pod, scratch PVC, native object
  Failed or Canceled    keepOnFailure: true keeps them
                        keepOnFailure: false deletes them

Retries, set on the RestoreTest. Only Failed is retried.
  Failed, attempts left    wait retries.backoff, then next attempt
  Failed, none left        run ends Failed
  Canceled                 not retried, run ends Failed
```

One attempt, in order. The list has only what the diagram does not show:

1. Resolve the scratch size (see [Scratch volume size](#scratch-volume-size)).
2. Create one scratch volume per source, owned by the `Restore`. Then create the native restore
   objects, targeting the scratch volumes.
3. When the native restore is done, create the check pod from the `RestoreCheck`. The restored
   volumes are mounted at the `restoredVolumes` paths.
4. Every check container must exit 0 for `Succeeded`. `restoreWindow` covers both waits: the
   native restore and the check pod.
5. A retry is a new `Restore`. It starts again at step 1, after `retries.backoff`.

### Phases

`Restore` phases: `Pending`, `Running`, `Succeeded`, `Failed`, `Canceled`.

The native tools use other words. Velero and Kopiur say `Completed`. Do not confuse them with
the operator's phases.

### Attempts and retries

- `retries.count` is the number of extra attempts after the first. The maximum is 10. The
  default is 0.
- `retries.backoff` is the wait between attempts. The default is `5m`.
- Each cancel writes a `RestoreCanceled` Event on the `Restore`.

### restoreWindow

`restoreWindow` is a deadline per attempt. It covers the native restore, the image pulls and
the check pod. The default is `45m`. The minimum is `1m`. Set it higher for large volumes or slow
image pulls. A check pod that finished before the deadline counts, even when the operator sees it
after the deadline.

The `spec` of a `Restore` cannot change after creation, so its status always belongs to the backups
in its spec. To test another backup, create a new `Restore`.

### keepOnFailure

With `keepOnFailure: true` and the last attempt not `Succeeded`, the operator keeps the check pod,
the native restore object and the scratch volume. Delete the `Restore` to remove them. Details:
[restoretest-guide.md](restoretest-guide.md#keeponfailure).

### overlapPolicy

Applies when a trigger fires while a run is still going: `Skip` (default), `Queue` or `Replace`.
Details: [restoretest-guide.md](restoretest-guide.md#overlappolicy).

## Triggers

A `RestoreTest` uses any combination of three triggers: a schedule (`triggers.cron`), after a
backup (`triggers.afterBackup.delay`), and manual (`triggers.manual` with the annotation
`bret.linogics.io/run-now`). Details: [restoretest-guide.md](restoretest-guide.md#triggers).

### Trigger flow

The diagram shows how a trigger becomes a run, and how a run becomes a `RunResult`.

```text
  cron tick ───────────────┐
  backup + delay ──────────┤
  run-now annotation ──────┘
                           ▼
┌──────────────────────────────────────────┐
│ Run already in progress?                 │
└────────┬─────────────────────────┬───────┘
      no │                         │ yes
         ▼                         ▼
  start one Restore       overlapPolicy
                          ├─ Skip     Event RunSkipped
                          ├─ Queue    keep one pending run
                          └─ Replace  Event RunReplaced, delete
                                      running Restore, start new
         ▼
  one Restore per attempt: <test>-<run>-a<n>
         ▼
  Restore ends Failed, and attempt <= retries.count?
     yes ─► wait retries.backoff ─► next attempt (a<n+1>)
     no  ─► RunResult: Succeeded or Failed (status.lastRun)
```

## Scope

Namespaces:

- The operator watches all namespaces by default.
- `watchNamespaces` in the chart values is a list. Empty means cluster-wide. The table shows what
  it limits.

| Mode | Controller cache | Pod and PVC rights | Velero rights |
|---|---|---|---|
| Cluster-wide (`watchNamespaces` empty) | All namespaces. | `ClusterRole`. | `ClusterRole`, when Velero is on. |
| `watchNamespaces` set | Only those namespaces. | One `Role` and `RoleBinding` per listed namespace. | None. Velero is off. |
| `watchNamespaces` set, Velero on | `Restore`, `RestoreTest` and `RestoreCheck`: only those namespaces. Pods and PVCs: all namespaces. | `ClusterRole`, because the scratch namespaces are not watched. | `Role` in `providers.velero.namespace`, even if it is not listed. |

- A `RestoreTest` in a namespace that is not watched is ignored. The operator writes nothing to it.
- Pods and volumes live in the namespace of the `Restore` or `RestoreTest`. For Velero, they live
  in the scratch namespace.
- With Velero on, the operator can read and create Secrets and ConfigMaps in every namespace. It
  copies the ones a check names into the scratch namespace, which is not watched.
- Velero backups live in the Velero namespace (`velero` by default). The operator reads
  backup objects there. In namespaced mode, this is the one cross-namespace read it needs.
