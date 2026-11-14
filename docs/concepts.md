# Concepts

This page explains what bret does and the rules it follows. The schema is in
[reference/api.md](reference/api.md). Behavior marked **planned** is design. It is not yet in the code.

## The problem

A backup that you never restored is not proven. bret restores your backups
on a schedule, checks the restored data, and tells you when the check fails.

## The three objects

| Kind | Short name | Purpose | Who writes it |
|---|---|---|---|
| `RestoreCheck` | `rcheck` | The check: a pod template that runs against restored data. Exit code 0 means pass. | You, once per app. |
| `RestoreTest` | `rtest` | The schedule and policy: which backups, which check, when, how many retries. | You. |
| `Restore` | `restore` | One restore attempt run: the native restore object plus the check pod. You create it by hand, or a `RestoreTest` creates it. | You, or the operator. |

A typical setup has one `RestoreCheck` per application. It also has one `RestoreTest` per backup
source that feeds the check.

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

| Provider | Native object created | Restore done when | Backup done when |
|---|---|---|---|
| `velero` | `velero.io/v1` `Restore` | `status.phase` is `Completed`, `PartiallyFailed`, `Failed` or `FailedValidation` | planned: `Backup` `status.phase` is `Completed` |
| `volsync` | `volsync.backube/v1alpha1` `ReplicationDestination` with a manual trigger | `status.latestImage.kind` is `PersistentVolumeClaim` | planned: `ReplicationSource` `status.lastSyncTime` changes |
| `kopiur` (alpha) | `kopiur.home-operations.com/v1alpha1` `Restore` | `status.phase` is `Completed` and condition `Ready` is `True` | `Snapshot` `status.phase` is `Succeeded` |

Kopiur is alpha. Setup steps are in
[backup-providers.md](backup-providers.md).

Enable providers in the Helm chart with `providers.<name>.enabled`. Kopiur is on by
default. Velero and VolSync are off by default. A `Restore` for a disabled provider fails with
reason `ProviderUnsupported`.

### Velero

The diagram shows the three namespaces of a Velero restore and what lives in each. Here the
`Restore` is in the namespace `data`.

```text
┌ velero ───────────────┐   ┌ data (your namespace) ─┐
│ Backup      (read)    │   │ RestoreTest            │
│ Restore     (created) │◄──│ Restore                │
└──────────┬────────────┘   │ RestoreCheck           │
           │                └───────────┬────────────┘
           ▼                            │ copies pod-security labels
┌ bret-scratch-<hash> ──────────────────▼────────────┐
│ restored PVC + PV   (only those of "data")         │
│ check pod           (no Secrets, no ConfigMaps)    │
└────────────────────────────────────────────────────┘
  hash = first 12 hex of sha256("<namespace>/<name>")
```

- The Velero namespace is `providers.velero.namespace`, default `velero`. The chart passes it to
  the operator as `VELERO_NAMESPACE`.
- The Velero `Restore` sets `includedNamespaces` to the `Restore` namespace. So no other namespace
  of the backup is restored.
- The hash covers the namespace. So equal `Restore` names in two namespaces never share data.
- The operator copies the `pod-security.kubernetes.io/*` labels before the check pod starts. The
  pod then runs under the same Pod Security rules as in your namespace.
- Velero restores at the original size. So `target.size` and `autoExpand` only gate the run.

A check for Velero must not set any of these. Such a check fails with `ProviderUnsupported`:

- `serviceAccountName`
- `imagePullSecrets`
- a configMap, secret, projected or persistentVolumeClaim volume
- a hostPath volume
- an env or envFrom reference to a Secret or ConfigMap

### VolSync

The diagram shows where the operator reads the backup settings, and what it creates in the
`Restore` namespace. The last line is the order in which it picks the mover UID.

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

- The `ReplicationDestination` uses a restic mover.
- The reason `RepositoryMissing` is set when the `ReplicationSource` has no
  `spec.restic.repository`, or when the Secret is not in the `Restore` namespace.
- The annotation and `runAsUser` are read from the check.
- The annotation value is a JSON map of volume key to UID. The operator reads the key `data`.
  A plain number such as `"999"` is a parse error. Example: `bret.linogics.io/mover-uid: '{"data": 999}'`.

## Volumes per restore

A `RestoreCheck` may list one entry in `restoredVolumes`, or use `restoreMount`. Each
`restoredVolumes` entry has `name`, `mountPath` and `sourcePVC` (the PVC in the backed-up
namespace). More than one entry fails with `ProviderUnsupported` in v1. Restoring several
volumes per check is a follow-up.

## The scratch-volume rule

The operator creates a volume for each restore attempt. The `Restore` owns the volume (except for Velero), because Velero creates the PVC itself in the scratch namespace.
A restore never writes into a live application volume.

The check pod mounts the scratch volume. It reads the restored data. It must not change
the application's own volume.

## Scratch volume size

The diagram shows how a `Restore` picks the size of its scratch volume, and which reason it sets
when it cannot.

```text
target.size set? ──yes─► use it ───────────────────────────┐
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

`target.size` on a `Restore` is optional. A `RestoreTest` needs it, see
[restoretest-guide.md](restoretest-guide.md#target). With an unknown source size, a set
`target.size` is used without the capacity check.

Two failures are reported as reasons on the `Restore`. Both end it as `Failed`, and nothing is
created:

- `SizeUnknown`: the source size is unknown and `target.size` is not set. It is also set for a
  running `Restore` from an older operator version that has no `status.resolvedSize`.
- `InsufficientCapacity`: `target.size`, or the `autoExpand` result, is smaller than the source
  size.

The source size comes from the provider:

- Kopiur: the data size of the `Snapshot`.
- Velero: the PVC request in the backup.
- VolSync: the request of the source PVC.

`target.autoExpand` has these fields:

- `percentage`: headroom on top of the source. Above 0, at most 1000.
- `min` and `max`: optional bounds. `min` must not exceed `max`.

Example: a 20Gi source with the default gives 22Gi. For a fixed size, set `target.size`. Or use
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
2. Create the scratch volume, owned by the `Restore`. Then create the native restore object,
   targeting the scratch volume.
3. When the native restore is done, create the check pod from the `RestoreCheck`. The restored
   volume is mounted at the check's `restoreMount` (or at the `restoredVolumes` path).
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
the check pod. The default is `45m`. Set it higher for large volumes or slow image pulls.

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
- **Planned:** Velero backups live in the Velero namespace (`velero` by default). The operator reads
  backup objects there. In namespaced mode, this is the one cross-namespace read it needs.
