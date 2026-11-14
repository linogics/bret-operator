# bret documentation

bret tests your backups. It restores a backup into a scratch volume. It runs a check
against the restored data in a throwaway pod. Then it records the result on the custom resource,
as Kubernetes Events and as Prometheus metrics. The operator does not move backup data. It
drives the restore object of Velero, VolSync or Kopiur. Then it runs your check.

## How it fits together

Each box is one step. The arrows show the order of work from your objects to the results.

```text
┌──────────────────────────────────────────────┐
│ You create                                   │
│   RestoreCheck   check pod template          │
│   RestoreTest    schedule, policy, retries   │
│   Restore        one attempt, or made by     │
│                  a RestoreTest               │
└──────────────────────┬───────────────────────┘
                       ▼
┌──────────────────────────────────────────────┐
│ bret controllers                             │
│   RestoreTest, Restore, RestoreCheck         │
└──────────────────────┬───────────────────────┘
                       ▼
┌──────────────────────────────────────────────┐
│ Provider restore object (one of)             │
│   velero   Restore                           │
│   volsync  ReplicationDestination            │
│   kopiur   Restore                           │
└──────────────────────┬───────────────────────┘
                       ▼
┌──────────────────────────────────────────────┐
│ Scratch volume (PVC), never the app volume   │
└──────────────────────┬───────────────────────┘
                       ▼
┌──────────────────────────────────────────────┐
│ Check pod   <restore>-check                  │
│   exit code 0 = pass                         │
└──────────────────────┬───────────────────────┘
                       ▼
┌──────────────────────────────────────────────┐
│ Results                                      │
│   status: Restore phase, RestoreTest lastRun │
│   Events: RestoreStarted, CheckPassed, ...   │
│   Prometheus: bret_restore_runs_total, ...   │
└──────────────────────────────────────────────┘
```

## Read in this order

1. [concepts.md](concepts.md): what the operator does, the restore test lifecycle, the rules.
2. [getting-started.md](getting-started.md): tutorial from an empty cluster to a first check.
3. [installation.md](installation.md): install and upgrade the operator (Helm or manifests).
4. [backup-providers.md](backup-providers.md): set up Velero, VolSync or Kopiur so restores work.
5. [checks.md](checks.md): write a `RestoreCheck` check.
6. [restoretest-guide.md](restoretest-guide.md): schedule and tune restore tests.
7. [testing-checks.md](testing-checks.md): run a check by hand before the operator does it.

## Reference

| Document | Content |
|---|---|
| [reference/api.md](reference/api.md) | Generated field reference for all three kinds. Do not edit by hand. |
| [checks/INDEX.md](../checks/INDEX.md) | Catalog of app checks, their volumes, checks and test status. |
| [best-practices.md](best-practices.md) | Rules for backups and restore tests. |
| [data-and-database.md](data-and-database.md) | Apps that have files and a database. Keep them in one restore point. |
| [troubleshooting.md](troubleshooting.md) | Known errors and their fixes. |

## Contributing

| Document | Content |
|---|---|
| [development.md](development.md) | Build, test, lint, generate docs, and the local kind setup. |
| [releasing.md](releasing.md) | How to cut a release and what it publishes. |
