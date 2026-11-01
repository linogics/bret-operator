# RestoreTest guide

A `RestoreTest` says what to restore, when, how often to retry, and what to keep. This page
explains each field with an example. The complete field list is in
[reference/api.md](reference/api.md).

**Status:** the schema is implemented. The behavior is **planned**: the controller does not run
restores yet. The rules below are the design the controller will follow.

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
  blueprintRef:
    name: minimal
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
tool to restore them. Setup per tool: [backup-providers.md](backup-providers.md).

### backupSelector

Picks which backups the test uses. Set either:

- `name`: one backup by name, or
- `labels`: all backups with these labels. The test uses the newest one.

Set at least one. The API rejects an empty selector.

Backups are looked up where the provider keeps them. For Velero that is the Velero namespace.

### blueprintRef

`name` of a `RestoreBlueprint` in the same namespace as the `RestoreTest`. The blueprint is the
check. See [blueprints.md](blueprints.md).

### triggers

Any combination of three triggers. A run starts when any one fires.

| Field | Example | Behavior |
|---|---|---|
| `cron` | `"17 3 * * *"` | 5-field cron in UTC. The pattern is checked by the API. |
| `afterBackup.delay` | `30m` | After a new matching backup completes at time `T`, a run starts at `T + delay`. Each backup is tested once, recorded in `status.handledBackups`. |
| `manual` | `true` | Enables the run-now annotation. A new value on `bret.linogics.io/run-now` starts one run. |

Pick the times on purpose. A test that starts while the backup is still writing can fail for no
good reason. Leave some margin after the backup window, as the `delay` above does.

### restoreWindow

Deadline for one attempt, default `45m`. It covers the native restore, image pulls, and the check
pod. When it passes, the attempt is canceled: the native object is deleted, the scratch volume is
deleted, and the attempt is marked `Canceled`.

Size it for the largest backup you test, plus image pulls. A database of 200 GB may need hours.
Check how long a restore takes in your environment before you set this.

### retries

| Field | Default | Meaning |
|---|---|---|
| `count` | `0` | Extra attempts after the first. The maximum is `10`. |
| `backoff` | `5m` | Wait between attempts. |

With `count: 2`, the operator makes up to three attempts. Each failed attempt is cleaned up
before the next one starts. Every retry and cancel writes an Event with a reason.

Retries help with transient failures: an image pull that times out, a busy storage backend.
They do not fix a broken check. A check that fails three times has a real problem.

### keepOnFailure

Default `false`. When `true` and the last attempt failed, the operator keeps:

- the check pod, so `kubectl logs` and `kubectl exec` still work,
- the native restore object,
- the scratch volume, so the restored data stays readable.

The kept objects get the label `bret.linogics.io/retained: "true"`. They are not removed
automatically. Inspect them, then delete them by hand. Earlier failed attempts are always removed.

Turn it on for a check you are still debugging. Leave it off in a long-running schedule, or
failed runs pile up volumes.

### overlapPolicy

What happens when a trigger fires while a run is still going.

| Value | Behavior |
|---|---|
| `Skip` (default) | Write an Event and drop the trigger. |
| `Queue` | Keep one pending run. It starts when the running one ends. Extra triggers are dropped. |
| `Replace` | Cancel the running attempt, clean it up, then start a new run. |

`Skip` is the safe default. Use `Queue` when runs are short and you never want to miss one.
Use `Replace` only when the newest backup must win.

## Status

| Field | Meaning |
|---|---|
| `status.lastRun.time` | When the last run ended. |
| `status.lastRun.result` | `Succeeded` or `Failed`. |
| `status.lastRun.reason` | Why, for example `ImagePull`, `PVCPending`, `CheckFailed`. |
| `status.nextRun` | Next cron tick (planned). |
| `status.handledBackups` | Backup names that `afterBackup` already ran for. |
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
  blueprintRef:
    name: orders-db-check
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
  blueprintRef:
    name: orders-db-check
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
  blueprintRef:
    name: orders-db-check
  triggers:
    manual: true
```

## Namespaces and scope

A `RestoreTest` lives in the namespace of the application it tests. Its pods and volumes go there
too. The operator watches all namespaces by default. A chart setting to limit this to a list of
namespaces is planned. See [concepts.md](concepts.md#scope).
