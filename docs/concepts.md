# Concepts

This page explains what bret-operator does and the rules it follows. The schema is in
[reference/api.md](reference/api.md). Behavior marked **planned** is design. It is not yet in the code.

## The problem

A backup that you never restored is not proven. bret-operator restores your backups
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
| `velero` | `velero.io/v1` `Restore` | `status.phase` is `Completed`, `PartiallyFailed` or `Failed` | `Backup` `status.phase` is `Completed` |
| `volsync` | `volsync.backube/v1alpha1` `ReplicationDestination` with a manual trigger | `status.lastManualSync` matches the trigger | `ReplicationSource` `status.lastSyncTime` changes |
| `kopiur` (alpha) | `kopiur.home-operations.com/v1alpha1` `Restore` | `status.phase` is `Completed` and condition `Ready` is `True` | `Snapshot` `status.phase` is `Succeeded` |

Kopiur is alpha. Setup steps are in
[backup-providers.md](backup-providers.md).

Enable providers in the Helm chart with `providers.<name>.enabled`. Kopiur is on by
default. Velero and VolSync are off by default. A `Restore` for a disabled provider fails with
reason `ProviderUnsupported`.

Velero and VolSync have these rules:

- Velero: the restore runs in the Velero namespace (`providers.velero.namespace`, default
  `velero`, passed to the operator as `VELERO_NAMESPACE`). Only the PVCs and PVs of the `Restore`
  namespace are restored (`includedNamespaces`), into the scratch namespace
  `bret-scratch-<hash>`. The hash is the first 12 hex characters of the SHA-256 of
  `<namespace>/<name>`. So equal Restore names in two namespaces never share data. The operator
  copies the `pod-security.kubernetes.io/*` labels of the `Restore` namespace onto the scratch
  namespace before the check pod starts. The pod then runs under the same Pod Security rules. A
  check for Velero must not set `serviceAccountName`, `imagePullSecrets`, a configMap or secret
  volume, a hostPath volume, or an env or envFrom reference to a Secret or ConfigMap. Such a check
  fails with `ProviderUnsupported`. Velero restores at the
  original size. So `target.size` and `autoExpand` only gate the run.
- VolSync: the operator creates a `ReplicationDestination` with a restic mover. The repository
  Secret name and the source PVC come from the backup's `ReplicationSource`, in the backup namespace
  (`backupRef.namespace`, or the `Restore` namespace when it is not set). The Secret itself must
  exist in the `Restore` namespace. A `ReplicationSource` without `spec.restic.repository` sets the
  reason `RepositoryMissing`. The mover UID comes from the check (annotation
  `bret.linogics.io/mover-uid`, else the first container's `runAsUser`). If neither is set, it falls back to 65534.

## Volumes per restore

A `RestoreCheck` may list one entry in `restoredVolumes`, or use `restoreMount`. Each
`restoredVolumes` entry has `name`, `mountPath` and `sourcePVC` (the PVC in the backed-up
namespace). More than one entry fails with `ProviderUnsupported` in v1. Restoring several
volumes per check is a follow-up.

## The scratch-volume rule

The operator creates a volume for each restore attempt. The `Restore` owns the volume.
A restore never writes into a live application volume.

The check pod mounts the scratch volume. It reads the restored data. It must not change
the application's own volume.

## Scratch volume size

`target.size` on a `Restore` is optional. When you omit it, the size comes from the source:

- Kopiur: the data size of the `Snapshot`.
- Velero: the PVC request in the backup.
- VolSync: the request of the source PVC.

`target.autoExpand` derives the size from the source size:
`clamp(source x (100 + percentage) / 100, min, max)`. The percentage is headroom on top of the
source. A 20Gi source with the default gives 22Gi. Its fields are `percentage` (above 0, at most
1000), and optional `min` and `max`. `min` must not exceed `max`. When you omit both `size` and
`autoExpand`, the default is `percentage: 10` with `min: 1Gi`. A result below the
source size fails with `InsufficientCapacity`. For a fixed size, set `target.size`. Or use
`percentage: 100, min: X, max: X` with X at least twice the source. The operator resolves the size
once and stores it in `status.resolvedSize`.

Two failures are reported as reasons on the `Restore`:

- `SizeUnknown`: the source size is unknown and `target.size` is not set.
- `InsufficientCapacity`: `target.size` is smaller than the source.

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
     ├─▶ check exits 0 ────────────────▶ Succeeded
     ├─▶ restore or check fails ───────▶ Failed
     └─▶ restoreWindow ends ───────────▶ Canceled

Terminal phase: cleanup runs after the status is stored.
  Succeeded             deletes pod, scratch PVC, native object
  Failed or Canceled    keepOnFailure: true keeps them
                        keepOnFailure: false deletes them

Retries, set on the RestoreTest. Only Failed is retried.
  Failed, attempts left wait retries.backoff, next attempt
  Canceled                  not retried
```

One attempt, in order:

1. Create the scratch volume, owned by the `Restore`.
2. Create the native restore object, targeting the scratch volume.
3. Wait until the native restore is done, or until `restoreWindow` runs out.
   On timeout: delete the native object and the scratch volume, and mark the attempt `Canceled`.
4. Create the check pod from the `RestoreCheck`. The restored volume is mounted at the
   check's `restoreMount` (or at each `restoredVolumes` path).
5. Wait for the check pod to finish, or for `restoreWindow` to run out.
6. Exit code 0 from every check container: phase `Succeeded`.
   Anything else: phase `Failed` for this attempt.
7. If the attempt failed and `retries.count` is not used up: wait `retries.backoff`, then
   start a new attempt at step 1. Otherwise, set phase `Failed`.
8. Clean up. On `Succeeded`, or on `Failed` with `keepOnFailure: false`, delete the pod,
   the volume and the native object. With `keepOnFailure: true`, keep them (see below).

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

With `keepOnFailure: true` and the last attempt failed, the operator keeps:

- the check pod (its logs and `kubectl exec` still work),
- the native restore object,
- the scratch volume, so the restored data stays readable.

Kept resources get the label `bret.linogics.io/retained: "true"`. The `RestoreTest` lists them in
`status.retainedResources`. The operator does not delete kept resources of the last attempt
automatically. Delete the `Restore` to remove them. A finalizer then deletes the check pod, the provider objects, the
Velero scratch namespace and the PVCs. There is no TTL in v1 (`retainedFor` is not implemented).

When the `RestoreTest` run settles, the operator removes earlier failed attempts. Only the last attempt is kept.

### overlapPolicy

Applies when a trigger fires while a run is still going.

| Value | Behavior |
|---|---|
| `Skip` (default) | Write an Event and drop the trigger. |
| `Queue` | Keep one pending run. It starts when the current run ends. Extra triggers are dropped. |
| `Replace` | Delete the running `Restore`, then start a new one. The operator does not wait for the old one's cleanup. Kubernetes garbage collection removes the objects it owned. |

Manual runs follow the same policy.

## Triggers

A `RestoreTest` uses any combination of these triggers.

| Trigger | Field | How it works |
|---|---|---|
| Schedule | `triggers.cron` | Standard 5-field cron, UTC. Each tick starts a run unless `overlapPolicy` says otherwise. |
| After backup | `triggers.afterBackup.delay` | When a matching backup completes at time `T`, a run starts at `T + delay`. The newest handled backup is recorded in `status.lastHandled`. A backup triggers a run only if it is newer than that (completion time, then name). Older backups never trigger. |
| Manual | `triggers.manual: true` | Set the annotation `bret.linogics.io/run-now` on the `RestoreTest` to any new value. A new value starts one run. Reusing a value does nothing. |

Example: a manual run needs no CLI. `kubectl annotate restoretest orders-db bret.linogics.io/run-now="$(date +%s)" --overwrite`

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
     yes ─▶ wait retries.backoff ─▶ next attempt (a<n+1>)
     no  ─▶ RunResult: Succeeded or Failed (status.lastRun)
```

## Scope

Namespaces:

- The operator watches all namespaces by default.
- `watchNamespaces` in the chart values is a list. Empty means cluster-wide. When you set it, the
  controller cache and the namespaced `Role` and `RoleBinding` objects are limited to those
  namespaces. The Velero rights stay in `providers.velero.namespace`, even if it is not listed.
  Pod and PVC rights are in the per-namespace `Role` objects, and the cache is limited to those
  namespaces. With Velero on, they stay in the `ClusterRole`, because the scratch namespaces are
  not watched.
- A `RestoreTest` in a namespace that is not watched is ignored. The operator writes nothing to it.
- Pods and volumes always live in the namespace of the `Restore` or `RestoreTest`.
- Velero backups live in the Velero namespace (`velero` by default). The operator reads
  backup objects there. In namespaced mode, this is the one cross-namespace read it needs.
