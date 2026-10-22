# Concepts

Status: design. Items marked **planned** are not in the code yet.

## What the operator does

- It triggers restores. It creates the native restore object of the chosen tool
  (Velero, VolSync or Kopiur), pointed at a scratch PVC.
- It waits for the native restore to finish.
- It runs your check (a `RestoreBlueprint`) in a pod that mounts the restored volume.
- It uses the check's exit code as the verdict. Exit 0 passes. Anything else fails.
- It writes the result to status, Events and metrics.

It does not copy, deduplicate or store backup data. It does not install Velero,
VolSync or Kopiur. You install them yourself.

## Providers

| Provider | Native object the operator creates | Done signal | Backup-completed signal |
|---|---|---|---|
| `velero` | `velero.io/v1` `Restore` | `status.phase` = `Completed`, `PartiallyFailed` or `Failed` | `Backup` `status.phase` = `Completed` |
| `volsync` | `volsync.backube/v1alpha1` `ReplicationDestination` (manual trigger) | `status.lastManualSync` matches the trigger | `ReplicationSource` `status.lastSyncTime` changes |
| `kopiur` (alpha) | `kopiur.home-operations.com/v1alpha1` `Restore` | Kopiur `Restore` status phase `Completed` and condition `Ready` | Kopiur snapshot completion (exact signal to verify) |

Kopiur is alpha. The design notes open Kopiur issue kopiur#233 about restore.
The Kopiur rows come from the spike. The exact backup-completion signal for Kopiur is
not pinned yet, so it is **unverified**.

## Scratch-PVC rule

Every restore writes into a PVC that the operator creates for that attempt.
The PVC is owned by the `Restore`. Live data is never a restore target in v1.
In-place restore is a later design.

The check pod mounts the scratch PVC. The check can read the restored data. It
cannot write to the application's PVC.

## Restore test lifecycle

One attempt, in order (design §7.3):

1. Create the scratch PVC, owned by the `Restore`.
2. Call the provider to create the native restore object, targeting the scratch PVC.
3. Wait until the native object is done, or until the deadline (`restoreWindow`).
   On deadline: clean up the native object, delete the scratch PVC, set phase `Canceled`.
4. When the native restore succeeds, create the check pod from the `RestoreBlueprint`.
   The restored PVC is mounted at `restoreMount`.
5. Wait for the pod to finish, or for the deadline.
6. Pod exit 0: phase `Succeeded`.
7. Pod exit not 0, or canceled: if attempts are left, wait `backoff` and start the next
   attempt at step 1. If no attempts are left, phase `Failed`.
8. Clean up. On `Succeeded`, or on `Failed` with `keepOnFailure: false`, delete the pod,
   the PVC and the native object. On `Failed` with `keepOnFailure: true`, keep them
   (see below).

### Phases

`Restore` phase: `Pending`, `Running`, `Succeeded`, `Failed`, `Canceled` (design §7.1).

Native phases are different. Velero uses `Completed`. Kopiur uses `Completed`.
Do not mix them up with the `Restore` phase.

### Attempts and retries

- `retries.count` is the number of extra attempts. `retries.backoff` is the wait between
  attempts (design §7.1 example: `count: 2`, `backoff: 10m`).
- The design example gives the test as up to 1 + `count` attempts. The exact count
  rule is **unverified** until the controller exists.
- Every retry and cancel writes an Event with a reason.

### restoreWindow

`restoreWindow` is a deadline per attempt. It covers the native restore, the check pod
and image pulls. A restore test cannot run longer than `restoreWindow` per attempt.

### keepOnFailure

When `keepOnFailure: true` and the last attempt fails, the operator keeps:

- the check pod (logs and `kubectl exec` still work),
- the native restore object,
- the scratch PVC (so the restored data stays).

It sets the label `restore.bret.linogics.io/retained: "true"` and writes an Event.
Retained resources are not deleted automatically. A user deletes them with
`kubectl delete restore`. A `retainedFor` TTL is an open question.

Earlier failed attempts are always cleaned up. Only the last attempt is kept.

### overlapPolicy

Applies when a trigger fires while a run is in progress.

| Value | Behaviour |
|---|---|
| `Skip` (default) | Write an Event and drop the tick. |
| `Queue` | Keep one pending run. It starts when the current run ends. Extra ticks are dropped. |
| `Replace` | Cancel the running attempt (same cleanup as a timeout), start a new one. |

Manual triggers follow the same policy.

## Triggers

| Trigger | How it works |
|---|---|
| `cron` | Standard 5-field cron, UTC. Each tick creates a `Restore` unless `overlapPolicy` blocks it. |
| `afterBackup` | The operator watches the provider's backup objects. A backup that completes at time `T` starts a run at `T + delay`. Each backup name is recorded in status, so each backup runs once. |
| `manual` | Set the annotation `restore.bret.linogics.io/run-now: "<nonce>"` on the `RestoreTest`. A new nonce starts one run. Reusing a nonce does nothing. No CLI needed. |

## Scope

Operator config (planned):

```yaml
watchNamespaces: []      # empty = cluster-wide (default)
# watchNamespaces: [data, billing]   # namespaced mode
```

- Empty list: cluster-wide. The `ClusterRole` is bound cluster-wide.
- Non-empty list: the controller cache only sees those namespaces. A `Role` and
  `RoleBinding` are created in each listed namespace. The Helm chart renders them.
- Pods and PVCs always go in the namespace of the `Restore` or `RestoreTest`.
- Velero backups live in the Velero namespace (`velero` by default). The operator reads
  backup objects there. In namespaced mode this is the one cross-namespace read
  permission, and it must be documented.
- Pitfall: a `RestoreTest` in a namespace missing from the list is ignored. The operator
  logs a warning and sets condition `WatchedNamespace=False` if it sees one.

Why hybrid: the controller cache and RBAC are set up at start-up. Adding namespaced
mode later is expensive. The design builds both modes now.
