# RestoreTest guide

A `RestoreTest` says what to restore, when, how often to retry, and what to keep. This page
explains each field with an example. The complete field list is in
[reference/api.md](reference/api.md).

**Status:** the controller implements the schedule, the after-backup and manual triggers, the
overlap policy and the retries. An end-to-end run on a live cluster is not yet verified.

A `RestoreTest` name can have at most 50 characters. The names of its `Restore` objects add up to 13 characters and must fit a 63-character label value.

## A complete example

Taken from [examples/restoretest.yaml](../examples/restoretest.yaml):

```yaml
apiVersion: bret.linogics.io/v1alpha1
kind: RestoreTest
metadata:
  name: orders-db-nightly
  namespace: data
spec:
  provider: kopiur
  backupSelector:
    labels:
      app.kubernetes.io/name: orders-db
  restoreCheckRef:
    name: minimal
  target:
    size: 20Gi
  triggers:
    cron: "17 3 * * *"
    afterBackup:
      delay: 30m
    manual: true
  restoreWindow: 45m
  retries:
    count: 2
    backoff: 10m
  keepOnFailure: true
  overlapPolicy: Skip
```

## Fields

### provider

One of `velero`, `volsync`, `kopiur`. The tool that made the backups. The operator uses the same
tool to restore them. Setup for each tool: [backup-providers.md](backup-providers.md).

The `RestoreTest` controller runs for `kopiur` only. A test with another provider gets the
condition reason `ProviderUnsupported` and starts no run. A `Restore` that you create by hand works
with all three providers.

### backupSelector

Picks which backups the test uses. Set either:

- `name`: one backup by name, or
- `labels`: all backups with these labels. The test uses the newest one.

Set at least one. The API rejects an empty selector.

The operator looks up Kopiur `Snapshot` objects in the namespace of the `RestoreTest`. Only
`Snapshot` objects in phase `Succeeded` count. `name` matches the `Snapshot` name. `labels` must all
match the `Snapshot` labels.

### restoreCheckRef

`name` of a `RestoreCheck` in the same namespace as the `RestoreTest`. The `RestoreCheck` is the
pod template that checks the restored data. See [checks.md](checks.md).

### target

`target` is the scratch volume that each run restores into. It has the same fields as
`Restore.spec.target`: `size`, `storageClassName` and `autoExpand`. The API does not require
`target.size`, but the `RestoreTest` controller does. Without it, or with a size of zero, the test
sets the condition reason `TargetMissing` and starts no run. `autoExpand` alone is not enough for a
`RestoreTest`. Only a `Restore` that you create by hand may omit `target.size`. The size rules for
a `Restore` are in [concepts.md](concepts.md#scratch-volume-size).

### triggers

Use any combination of three triggers. A run starts when any one of them fires.

| Field | Example | Behavior |
|---|---|---|
| `cron` | `"17 3 * * *"` | 5-field cron in UTC. The API checks the pattern. Each tick starts a run unless `overlapPolicy` says otherwise. |
| `afterBackup.delay` | `30m` | After a new matching backup completes at time `T`, a run starts at `T + delay`. The newest handled backup is recorded in `status.lastHandled`. A backup triggers a run only if it is newer than that (completion time, then name). Older backups never trigger. Backups that completed before the `RestoreTest` was created never trigger either. |
| `manual` | `true` | Enables the run-now annotation. Any changed value on `bret.linogics.io/run-now` starts one run. Setting the same value again while it is still set does nothing. |

A manual run needs no CLI:

```bash
kubectl annotate restoretest orders-db bret.linogics.io/run-now="$(date +%s)" --overwrite
```

The flow from a trigger to a run result is in [concepts.md](concepts.md#trigger-flow).

Pick the times on purpose. A test that starts while the backup is still writing can fail for no
good reason. Leave some margin after the backup window, as the `delay` above does.

### restoreWindow

Deadline for one attempt. The default is `45m`. It covers the native restore, image pulls, and the check
pod. When it passes, the operator marks the attempt `Canceled`. It deletes the native object and
the scratch volume, unless `keepOnFailure` keeps them.

Size it for the largest backup that you test, plus image pulls. A database of 200 GB may need hours.
Check how long a restore takes in your environment before you set this value.

### retries

| Field | Default | Meaning |
|---|---|---|
| `count` | `0` | Extra attempts after the first. The maximum is `10`. |
| `backoff` | `5m` | Wait between attempts. |

With `count: 2`, the operator makes up to three attempts. It cleans up each failed attempt
before it starts the next one. A canceled attempt writes a `RestoreCanceled` Event on its `Restore`.

Retries help with transient failures, such as an image pull that times out, or a busy storage backend.
They do not fix a broken check. A check that fails three times has a real problem.

### keepOnFailure

Default `false`. When `true` and the last attempt failed, the operator keeps:

- the check pod, so `kubectl logs` and `kubectl exec` still work,
- the native restore object,
- the scratch volume, so the restored data stays readable.

The kept objects get the label `bret.linogics.io/retained: "true"`. The `RestoreTest` lists them
in `status.retainedResources`. The operator does not remove them automatically. v1 has no TTL
(`retainedFor` is not implemented). Inspect them, then delete the `Restore`. Its finalizer deletes
the check pod, the provider objects, the Velero scratch namespace and the PVCs.

When the run settles, the operator removes earlier failed attempts. Only the last attempt is kept.

Turn it on for a check that you still debug. Leave it off in a long-running schedule. Otherwise,
failed runs pile up volumes.

### overlapPolicy

What happens when a trigger fires while a run is still going.

| Value | Behavior |
|---|---|
| `Skip` (default) | Write an Event and drop the trigger. |
| `Queue` | Keep one pending run. It starts when the running one ends. Extra triggers are dropped. |
| `Replace` | Delete the running `Restore`, then start a new run. The operator does not wait for the old one's cleanup. Kubernetes garbage collection removes the objects it owned. |

Manual runs follow the same policy.

`Skip` is the safe default. Use `Queue` when runs are short and you never want to miss one.
Use `Replace` only when the newest backup must win.

## Status

| Field | Meaning |
|---|---|
| `status.lastRun.time` | When the last run ended. |
| `status.lastRun.result` | `Succeeded` or `Failed`. |
| `status.lastRun.reason` | Free text. It copies the message of the `Restore` that ended the run. |
| `status.nextRun` | Next cron tick. |
| `status.lastHandled` | Name and time of the newest backup that `afterBackup` ran for. |
| `status.retainedResources` | Names of kept objects when `keepOnFailure` kept a failed run. |
| `status.conditions` | Standard conditions. The `Ready` reason is the run result (`Succeeded` or `Failed`), `Running`, or a blocking reason such as `TargetMissing` or `BackupNotFound`. |

Read it with:

```bash
kubectl -n data get restoretest orders-db-nightly -o jsonpath='{.status.lastRun}{"\n"}'
```

## Recipes

### Test only the newest backup of one app, nightly

```yaml
spec:
  provider: kopiur
  backupSelector:
    labels:
      app.kubernetes.io/name: orders-db
  restoreCheckRef:
    name: orders-db-check
  target:
    size: 20Gi
  triggers:
    cron: "30 4 * * *"
  restoreWindow: 60m
  retries:
    count: 1
    backoff: 15m
```

### Test every new backup, and debug failures

```yaml
spec:
  provider: kopiur
  backupSelector:
    labels:
      app.kubernetes.io/name: orders-db
  restoreCheckRef:
    name: orders-db-check
  target:
    size: 20Gi
  triggers:
    afterBackup:
      delay: 20m
  keepOnFailure: true
```

### Run on demand only

```yaml
spec:
  provider: kopiur
  backupSelector:
    name: orders-db-data-1
  restoreCheckRef:
    name: orders-db-check
  target:
    size: 20Gi
  triggers:
    manual: true
```

## Namespaces and scope

A `RestoreTest` lives in the namespace of the application that it tests. Its pods and volumes go there
too. The operator watches all namespaces by default. The chart value `watchNamespaces` (a list)
limits the operator to those namespaces. Empty means cluster-wide. See
[concepts.md](concepts.md#scope).
