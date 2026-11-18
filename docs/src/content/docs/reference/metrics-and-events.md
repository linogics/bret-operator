---
title: Metrics and events
description: The Prometheus metrics bret exports, an alert for stale restore tests, and every Event reason.
---

## Metrics

The operator serves metrics on port 8443 of the Service `bret-operator-metrics`. With
`metrics.secure: true` the scraper needs a bearer token. See
[Metrics scraping](/start/installation/#metrics-scraping).

### `bret_restoretest_last_success_timestamp_seconds`

- **Type:** gauge.
- **Labels:** `restoretest`, `namespace`.
- **Value:** Unix time of the last run of the `RestoreTest` that passed. The series appears after
  the first passing run and goes away when the `RestoreTest` is deleted.

This is the metric to alert on. A backup that has not passed a restore test for too long is the
failure bret exists to catch. The alert fires when the last pass of a daily test is older than
two days:

```yaml
groups:
  - name: bret
    rules:
      - alert: BretRestoreTestStale
        expr: time() - bret_restoretest_last_success_timestamp_seconds > 2 * 86400
        for: 15m
        labels:
          severity: warning
        annotations:
          summary: "Restore test {{ $labels.namespace }}/{{ $labels.restoretest }} has not passed for 2 days"
```

A `RestoreTest` that never passed has no series, so this alert does not see it. Watch
`status.lastRun` of new tests, or alert on `bret_restore_runs_total` below.

### `bret_restore_runs_total`

- **Type:** counter.
- **Labels:** `provider` (`velero`, `volsync`, `kopiur`), `result` (`succeeded`, `failed`,
  `canceled`).
- **Value:** finished restore attempts. Every retry counts.

Failed attempts in the last day, per provider:

```text
sum by (provider) (increase(bret_restore_runs_total{result!="succeeded"}[1d]))
```

### `bret_restore_duration_seconds`

- **Type:** histogram, buckets from 15 s to 64 min.
- **Labels:** `provider`, `result`.
- **Value:** time from the start of an attempt to its end: the native restore plus the check pod.

Use it to set `restoreWindow`. The 95th percentile over a week:

```text
histogram_quantile(0.95, sum by (le, provider) (rate(bret_restore_duration_seconds_bucket[7d])))
```

## Events

bret writes Events on the `Restore` and the `RestoreTest`. A failure reason is written once, when
it first appears.

```bash
kubectl -n <namespace> get events --field-selector involvedObject.name=<name>
```

### Progress events

| Reason | Type | On | Means |
|---|---|---|---|
| `RestoreStarted` | Normal | `Restore` | bret created the native restore object. The message names it. |
| `CheckPassed` | Normal | `Restore` | The check pod exited 0. |
| `CheckFailed` | Warning | `Restore` | The check pod exited non-zero. Followed by `RestoreFailed`. |
| `RestoreSucceeded` | Normal | `Restore` | The attempt ended `Succeeded`. |
| `RestoreFailed` | Warning | `Restore` | The attempt ended `Failed`. See [`RestoreFailed`](/reference/failure-reasons/#restorefailed). |
| `RestoreCanceled` | Warning | `Restore` | `restoreWindow` ran out. See [`RestoreCanceled`](/reference/failure-reasons/#restorecanceled). |
| `RunSkipped` | Normal | `RestoreTest` | A trigger fired while a run was going (`overlapPolicy: Skip`), or while a run was already queued (`Queue`). |
| `RunReplaced` | Normal | `RestoreTest` | `overlapPolicy: Replace` replaced the running attempt. |
| `WaitingForBackups` | Normal | `RestoreTest` | With `afterBackup`, some sources have a new backup and others do not yet. Also the reason of the `BackupsPending` condition. |

`Running` is not an Event. It is the reason of the `RestoreTest` `Ready` condition while an
attempt runs or waits for a retry.

### Failure events

Each reason on [Failure reasons](/reference/failure-reasons/) is also written as a Warning Event
on the object that shows it, except `RestoreNameInUse`, which is only a condition.

## Conditions

| Object | Condition | Reasons |
|---|---|---|
| `Restore` | `Ready` | `Running` while an attempt runs, `Succeeded`, `Failed`, `Canceled` at the end, or a failure reason |
| `RestoreTest` | `Ready` | `Running`, the result of the last run (`Succeeded` or `Failed`), or a failure reason |
| `RestoreTest` | `BackupsPending` | `WaitingForBackups` |
| `RestoreCheck` | `Ready` | `Valid` or `Invalid` |
