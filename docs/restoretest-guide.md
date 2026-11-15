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
  name: nextcloud-nightly
  namespace: nextcloud
spec:
  provider: kopiur
  sources:
    - sourcePVC: html
      backupSelector:
        labels:
          app.kubernetes.io/name: nextcloud-html
      storageClassName: fast-ssd
      size: 20Gi
    - sourcePVC: pgdata
      backupSelector:
        labels:
          app.kubernetes.io/name: nextcloud-pgdata
      storageClassName: fast-ssd
      size: 10Gi
  restoreCheckRef:
    name: nextcloud
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

### sources

One entry per PVC that the app has. Each run restores all of them together. The `sourcePVC` values
must be exactly the `sourcePVC` values of the `restoredVolumes` in the `RestoreCheck`, or the `Restore`
ends Failed with `SourceMissing`. See [concepts.md](concepts.md#which-pvc-gets-restored).

Each source has a `sourcePVC`, a `backupSelector` and the scratch volume fields (below).

A one-volume app has one entry:

```yaml
sources:
  - sourcePVC: data
    backupSelector:
      labels:
        app.kubernetes.io/name: orders-db
```

### backupSelector

Picks which backups one source uses. Set either:

- `name`: one backup by name, or
- `labels`: all backups with these labels. The test uses the newest one.

Set at least one. The API rejects an empty selector.

The operator looks up Kopiur `Snapshot` objects in the namespace of the `RestoreTest`. Only
`Snapshot` objects in phase `Succeeded` count. `name` matches the `Snapshot` name. `labels` must all
match the `Snapshot` labels. One `Snapshot` holds one PVC, so use one selector per source. Each run
restores the newest backup of every source.

If a source has no matching backup, the test sets the reason `BackupNotFound`. The message names
the sources without a backup.

### restoreCheckRef

`name` of a `RestoreCheck` in the same namespace as the `RestoreTest`. The `RestoreCheck` is the
pod template that checks the restored data. See [checks.md](checks.md).

### Scratch volume per source

Each source is restored into its own scratch volume. The fields are in the source itself, next to
`backupSelector`: `size`, `storageClassName` and `autoExpand`. All are optional. Without `size`
and `autoExpand`, the source size and the default headroom decide. The size rules are in
[concepts.md](concepts.md#scratch-volume-size).

### triggers

Use any combination of three triggers. A run starts when any one of them fires.

| Field | Example | Behavior |
|---|---|---|
| `cron` | `"17 3 * * *"` | 5-field cron in UTC. The API checks the pattern. Each tick starts a run unless `overlapPolicy` says otherwise. |
| `afterBackup.delay` | `30m` | Every source needs a new matching backup. When the last of them completes at time `T`, a run starts at `T + delay`. The newest handled backup of each source is recorded in `status.lastHandled`. A backup counts as new only if it is newer than that (completion time, then name). Older backups never trigger. Backups that completed before the `RestoreTest` was created never trigger either. While some sources have a new backup and others do not, the reason is `WaitingForBackups`. |
| `manual` | `true` | Enables the run-now annotation. Any changed value on `bret.linogics.io/run-now` starts one run. Setting the same value again while it is still set does nothing. |

A manual run needs no CLI:

```bash
kubectl annotate restoretest nextcloud-nightly bret.linogics.io/run-now="$(date +%s)" --overwrite
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
| `status.lastHandled` | Per source: name and time of the newest backup that `afterBackup` ran for. |
| `status.retainedResources` | Names of kept objects when `keepOnFailure` kept a failed run. |
| `status.conditions` | Standard conditions. The `Ready` reason is the run result (`Succeeded` or `Failed`), `Running`, or a blocking reason: `BackupNotFound` (names the sources without a backup), `WaitingForBackups` (`afterBackup` waits for sources that have no new backup yet), or `SourceMissing` (the sources and the check volumes differ). |

Read it with:

```bash
kubectl -n nextcloud get restoretest nextcloud-nightly -o jsonpath='{.status.lastRun}{"\n"}'
```

## Recipes

The recipes show `sources`. The rest of the spec is as above.

### Test only the newest backups of one app, nightly

```yaml
spec:
  provider: kopiur
  sources:
    - sourcePVC: html
      backupSelector:
        labels:
          app.kubernetes.io/name: nextcloud-html
    - sourcePVC: pgdata
      backupSelector:
        labels:
          app.kubernetes.io/name: nextcloud-pgdata
  restoreCheckRef:
    name: nextcloud
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
  sources:
    - sourcePVC: html
      backupSelector:
        labels:
          app.kubernetes.io/name: nextcloud-html
    - sourcePVC: pgdata
      backupSelector:
        labels:
          app.kubernetes.io/name: nextcloud-pgdata
  restoreCheckRef:
    name: nextcloud
  triggers:
    afterBackup:
      delay: 20m
  keepOnFailure: true
```

### Run on demand only, with named backups

```yaml
spec:
  provider: kopiur
  sources:
    - sourcePVC: html
      backupSelector:
        name: html-1
    - sourcePVC: pgdata
      backupSelector:
        name: pgdata-1
  restoreCheckRef:
    name: nextcloud
  triggers:
    manual: true
```

### One volume

```yaml
spec:
  provider: kopiur
  sources:
    - sourcePVC: data
      backupSelector:
        labels:
          app.kubernetes.io/name: orders-db
  restoreCheckRef:
    name: minimal
  triggers:
    manual: true
```

## Namespaces and scope

A `RestoreTest` lives in the namespace of the application that it tests. Its pods and volumes go there
too. The operator watches all namespaces by default. The chart value `watchNamespaces` (a list)
limits the operator to those namespaces. Empty means cluster-wide. See
[concepts.md](concepts.md#scope).
