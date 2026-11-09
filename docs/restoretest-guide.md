# RestoreTest guide

A `RestoreTest` says what to restore, when, how often to retry, and what to keep. This page
explains each field with an example. The complete field list is in
[reference/api.md](reference/api.md).

**Status:** implemented. The controller runs the schedule, the after-backup and manual triggers,
the overlap policy and the retries. It has not yet run end to end on a cluster.

## A complete example

Taken from [examples/restoretest.yaml](../examples/restoretest.yaml):

```yaml
apiVersion: bret.linogics.io/v1alpha1
kind: RestoreTest
metadata:
  name: orders-db-nightly
  namespace: data
spec:
  provider: volsync
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

### backupSelector

Picks which backups the test uses. Set either:

- `name`: one backup by name, or
- `labels`: all backups with these labels. The test uses the newest one.

Set at least one. The API rejects an empty selector.

The operator looks up backups where the provider keeps them. For Velero, that is the Velero namespace.

### restoreCheckRef

`name` of a `RestoreCheck` in the same namespace as the `RestoreTest`. The check is the
check. See [checks.md](checks.md).

### target

`target` is the scratch volume that each run restores into. It has the same fields as
`Restore.spec.target`: `size`, `storageClassName` and `autoExpand`. In v1, `target.size` is
required. Without it, the test sets the condition reason `TargetMissing` and starts no run.
The size rules for a `Restore` are in [concepts.md](concepts.md#scratch-volume-size).

### triggers

Use any combination of three triggers. A run starts when any one of them fires.

| Field | Example | Behavior |
|---|---|---|
| `cron` | `"17 3 * * *"` | 5-field cron in UTC. The API checks the pattern. |
| `afterBackup.delay` | `30m` | After a new matching backup completes at time `T`, a run starts at `T + delay`. A backup triggers a run only if it is newer than `status.lastHandled` (completion time, then name). Older backups never trigger. |
| `manual` | `true` | Enables the run-now annotation. A new value on `bret.linogics.io/run-now` starts one run. |

Pick the times on purpose. A test that starts while the backup is still writing can fail for no
good reason. Leave some margin after the backup window, as the `delay` above does.

### restoreWindow

Deadline for one attempt. The default is `45m`. It covers the native restore, image pulls, and the check
pod. When it passes, the attempt is canceled. The operator deletes the native object and the scratch volume,
and marks the attempt `Canceled`.

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

The kept objects get the label `bret.linogics.io/retained: "true"`. The operator does not remove them
automatically, and v1 has no TTL (`retainedFor` is not implemented). Inspect them, then delete them
by hand. Or delete the `Restore`. Its finalizer removes the kept objects. Earlier failed attempts
are removed when the run settles.

Turn it on for a check that you still debug. Leave it off in a long-running schedule. Otherwise,
failed runs pile up volumes.

### overlapPolicy

What happens when a trigger fires while a run is still going.

| Value | Behavior |
|---|---|
| `Skip` (default) | Write an Event and drop the trigger. |
| `Queue` | Keep one pending run. It starts when the running one ends. Extra triggers are dropped. |
| `Replace` | Delete the running `Restore`, then start a new run. The operator does not wait for the old one's cleanup. Kubernetes garbage collection removes the objects it owned. |

`Skip` is the safe default. Use `Queue` when runs are short and you never want to miss one.
Use `Replace` only when the newest backup must win.

## Status

| Field | Meaning |
|---|---|
| `status.lastRun.time` | When the last run ended. |
| `status.lastRun.result` | `Succeeded` or `Failed`. |
| `status.lastRun.reason` | Why, for example `ImagePull`, `PVCPending`, `CheckFailed`. |
| `status.nextRun` | Next cron tick. |
| `status.lastHandled` | Name and time of the newest backup that `afterBackup` ran for. |
| `status.retainedResources` | Names of kept objects when `keepOnFailure` kept a failed run. |
| `status.conditions` | Standard conditions. `WatchedNamespace` in namespaced mode (planned). |

Read it with:

```bash
kubectl -n data get restoretest orders-db-nightly -o jsonpath='{.status.lastRun}{"\n"}'
```

## Recipes

### Test only the newest Velero backup of one namespace, nightly

```yaml
spec:
  provider: velero
  backupSelector:
    labels:
      velero.io/schedule-name: nightly-data
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

### Test every new VolSync backup, and debug failures

```yaml
spec:
  provider: volsync
  backupSelector:
    name: orders-db-backup
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
