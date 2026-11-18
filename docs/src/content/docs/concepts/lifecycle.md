---
title: Restore test lifecycle
description: Phases, attempts, retries, restore windows, overlap policy and triggers.
---

## The restore test lifecycle

The diagram shows the phases of one `Restore`, with the retry and keep rules that apply after it ends.

```d2 title="Restore phases from Pending to Succeeded, Failed or Canceled"
direction: down
pending: "Pending\nRestore created, cleanup finalizer added"
running: "Running\nnative restore, then check pod"
succeeded: Succeeded
failed: Failed
canceled: Canceled
pending -> running
running -> succeeded: check exits 0
running -> failed: restore or check fails
running -> canceled: restoreWindow ends
```

In a terminal phase, cleanup runs after the status is stored:

| Phase | Cleanup |
|---|---|
| `Succeeded` | Deletes the check pod, the scratch PVC and the native restore object. |
| `Failed` or `Canceled` | `keepOnFailure: true` keeps them. `keepOnFailure: false` deletes them. |

Retries are set on the `RestoreTest`. Only `Failed` is retried:

| Result | Next |
|---|---|
| `Failed`, attempts left | Wait `retries.backoff`, then the next attempt. |
| `Failed`, none left | The run ends `Failed`. |
| `Canceled` | Not retried. The run ends `Failed`. |

One attempt, in order. The list has only what the diagram does not show:

1. Resolve the scratch size (see [Scratch volume size](/concepts/volumes/#scratch-volume-size)).
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
[restoretest-guide.md](/guides/restore-tests/#keeponfailure).

### overlapPolicy

Applies when a trigger fires while a run is still going: `Skip` (default), `Queue` or `Replace`.
Details: [restoretest-guide.md](/guides/restore-tests/#overlappolicy).

## Triggers

A `RestoreTest` uses any combination of three triggers: a schedule (`triggers.cron`), after a
backup (`triggers.afterBackup.delay`), and manual (`triggers.manual` with the annotation
`bret.linogics.io/run-now`). Details: [restoretest-guide.md](/guides/restore-tests/#triggers).

### Trigger flow

The diagram shows how a trigger becomes a run, and how a run becomes a `RunResult`.

```d2 title="How a trigger becomes a run and a run becomes a RunResult"
direction: down
cron: cron tick
backup: backup + delay
runnow: run-now annotation
busy: "Run already in progress?" {shape: diamond}
cron -> busy
backup -> busy
runnow -> busy
policy: overlapPolicy {
  skip: "Skip\nEvent RunSkipped"
  queue: "Queue\nkeep one pending run"
  replace: "Replace\nEvent RunReplaced,\ndelete running Restore"
}
start: "start one Restore\n<test>-<run>-a<n>"
busy -> start: no
busy -> policy: yes
policy.queue -> start: when the run ends
policy.replace -> start
retry: "Restore Failed and\nattempt <= retries.count?" {shape: diamond}
start -> retry: Restore ends
wait: wait retries.backoff
retry -> wait: yes
wait -> start: "next attempt a<n+1>"
result: "RunResult: Succeeded or Failed\n(status.lastRun)"
retry -> result: no
```
