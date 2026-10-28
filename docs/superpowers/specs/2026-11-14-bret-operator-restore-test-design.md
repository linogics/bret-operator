# bret-operator: Automated Restore Testing — Design

Date: 2026-11-14
Status: Draft, pending approval

## 1. Intent and Why

Backups are only useful if they restore. Teams run Velero, VolSync and
Kopiur, but rarely prove that a restore produces a working application.
`bret-operator` is a Kubernetes operator that **triggers restores** from
those three tools and **verifies them automatically** by running a
user-defined restore check in a throwaway pod. Failures show up as
status and metrics before a real disaster does.

The operator does not move data itself. It drives each tool's own
restore object and then checks the result.

## 2. Goals

- One API (`Restore`, `RestoreTest`, `RestoreBlueprint`) for Velero,
  VolSync and Kopiur.
- Scheduled, event-driven (after backup) and manual restore tests.
- A check that can be any container image in any language: the operator
  starts a pod with init containers and containers, mounts the restored
  volume, and uses the exit code as the verdict.
- Hard time limits on a restore test, with configurable retry.
- Restores never touch live data.
- Scope can be a set of namespaces or the whole cluster, decided by
  configuration, not by a rebuild.
- Results visible on CR status, as Kubernetes Events and as Prometheus
  metrics.
- End-to-end tests on kind (podman provider) that seed a database,
  back it up, restore it, and check the result.

## 3. Non-Goals

- Copying or deduplicating backup data inside the operator.
- Managing installation of Velero, VolSync or Kopiur. Tests install them;
  users install them themselves.
- Restoring production workloads in place. v1 restores only into scratch
  PVCs (see §6.4). In-place restore is a later design.
- Notifications (webhook, chat). Results go to status, events and metrics.
- Verifying data beyond what the user's check script tests.

## 4. Anti-Goals

- A restore test that passes without running the user's check.
- A restore test that can write to a PVC the application uses.
- Silent cleanup: a failed test must never delete its evidence without
  the user having asked for that.
- A restore test that runs forever. Every test has a deadline.

## 5. Constraints

- Language: Go, built with Operator SDK (controller-runtime).
- License: BSD-3-Clause for this project. Kopiur is AGPL-3.0-only. We
  only call its CRDs over the Kubernetes API. We do not link or vendor
  its code.
- S3-compatible storage for tests: Versity Gateway v1.10.0 (posix backend,
  versioning on). MinIO is not used because its images need a login now.
  The lexington repo makes the same choice (`internal/testcontainers/tests3`).
- Kopiur is alpha (`kopiur.home-operations.com/v1alpha1`). Community
  note: open restore issue kopiur#233. Kopiur support ships marked alpha.
- Local e2e: kind with `KIND_EXPERIMENTAL_PROVIDER=podman`. The kind
  provider on podman is experimental upstream.
- Restore test pods run user-supplied images. This is a trust boundary
  (see §11).
- Velero CSI backups without data movement (`--snapshot-move-data`) keep
  volume data in the cluster only. Off-cluster recovery needs data movement
  enabled (spike, §16).
- Mover pods (VolSync, Kopiur) must run as the database uid (999 for
  postgres:17). The default mover uid cannot read a database data dir (§16).
- No pod `fsGroup` on a database data directory. It changes the directory to
  2770 and Postgres refuses to start (§16).

## 6. Approaches Considered

Chosen direction is recorded in §6.5. The two other shapes below were
discussed and dropped.

### 6.1 Per-tool CRDs (rejected)

`VeleroRestore`, `VolSyncRestore`, `KopiurRestore` as three kinds. More
kinds to maintain, duplicated test logic, and users must learn three
APIs for one job.

### 6.2 Template generator only (rejected)

The operator renders native restore YAML and the user applies it. No
status loop, no timeout, no retry, no metrics. Does not meet the restore
test goal.

### 6.3 Unified CRD over native tool objects (chosen)

One `Restore` kind with `spec.provider: velero | volsync | kopiur`. The
operator creates the matching native object (Velero `Restore`, VolSync
`ReplicationDestination`, Kopiur `Restore`) and reads its status through
a provider adapter. Adapters implement one interface (§7.2).

### 6.4 Scratch-PVC restore only (chosen as a rule)

Every restore writes into a PVC created by the operator for that attempt.
Live data is never a restore target in v1.

### 6.5 Decision summary

- Operator triggers only. No data movement.
- One API across providers.
- Trigger-only architecture, scratch PVCs, and a dedicated check pod.

## 7. Architecture

### 7.1 Custom resources

Group: `restore.bret.linogics.io/v1alpha1` (name to confirm; see §13).

**`Restore`** — one restore run, manual or started by a test.

```yaml
apiVersion: restore.bret.linogics.io/v1alpha1
kind: Restore
metadata:
  name: orders-db-manual
  namespace: data           # used for the scratch PVC and pod (§8)
spec:
  provider: velero          # velero | volsync | kopiur
  source:
    backupRef: { name: orders-db-2026-11-14, namespace: velero }  # provider-specific
  target:
    storageClassName: fast-ssd
    size: 20Gi
status:
  phase: Pending | Running | Succeeded | Failed | Canceled
  attempt: 1
  conditions: []
  providerRef: { kind: Restore, name: ..., namespace: ... }
```

**`RestoreBlueprint`** — the check. A full `PodSpec`-shaped template.

```yaml
apiVersion: restore.bret.linogics.io/v1alpha1
kind: RestoreBlueprint
metadata:
  name: postgres-check
  namespace: data
spec:
  restoreMount: /restore      # where the restored volume appears in every container
  initContainers: []          # any image; run before the check containers
  containers:                 # any image, any language; exit 0 = pass
  - name: check
    image: postgres:17
    command: ["/bin/sh", "-c", "/restore/start.sh && psql ... "]
  volumes: []                 # extra volumes (ConfigMap, Secret) besides the restored one
```

The operator adds the restored volume to the pod and mounts it at
`restoreMount` in every container.

Two additions from the blueprint drafts (proposed, not yet in the types):

- `spec.restoredVolumes: [{name, mountPath}]` for apps with more than one PVC
  (for example files plus database). Each entry is a separate PVC restored
  from the same backup point. `restoreMount` is used for single-PVC apps only.
- Native sidecar: a database may run as an `initContainers` entry with
  `restartPolicy: Always`, gated by a `startupProbe` (Kubernetes 1.29+). It
  keeps running while the check containers run. This departs from the rule
  that init containers run and exit before the checks.
- `spec.scratchVolumes: [{name, mountPath, storageClassName, size}]` for
  staging space the check needs (for example to unpack or decompress a restore).
  The operator creates one PVC per entry for each attempt, owned by the
  `Restore`. It deletes them after the attempt, or keeps them with
  `keepOnFailure`. `size` is required. Extra volumes that are static
  (ConfigMap, Secret, small `emptyDir`) use the blueprint's `volumes` and
  `volumeMounts`, no operator change.
- Pod-level fields, the rest of the PodSpec the check pod needs:
  `securityContext` (pod `fsGroup`, `runAsUser`, and so on), `serviceAccountName`,
  `imagePullSecrets`, `nodeSelector`, `tolerations`, `affinity`, `priorityClassName`,
  `runtimeClassName`. The blueprint is a PodSpec-shaped template, so these apply to the
  check pod as written. The mover settings on the native tool objects are separate.

Sizing:

- Restored volumes default to the capacity of the source PVC recorded at backup
  time. Kopiur reports the logical size in `status.stats.sizeBytes`, which can
  be used to check fit.
- Scratch volumes need an explicit `size`. Unpacking a compressed archive can
  need about twice the data size.
- If the backup data does not fit the target, the attempt fails before the
  restore starts, with reason `InsufficientCapacity`. It does not wait for an
  `ENOSPC` in the check.

Restore layout convention: `restoreMount` is the app's original mount path,
so absolute paths in app config still resolve. A Postgres source keeps its
data in `<mount>/pgdata` (see §16). Scripts can be Bash, Python, Go binaries
or anything else; the operator only looks at the exit code.

**`RestoreTest`** — schedule and policy. Creates `Restore` objects.

```yaml
apiVersion: restore.bret.linogics.io/v1alpha1
kind: RestoreTest
metadata:
  name: orders-db-nightly
  namespace: data
spec:
  provider: velero
  backupSelector: { labels: { app: orders-db } }   # or explicit backupRef
  blueprintRef: { name: postgres-check }
  triggers:
    cron: "0 3 * * *"                 # optional
    afterBackup: { delay: 2h }        # optional
    manual: true                      # honours metadata annotation, see §7.4
  restoreWindow: 45m                  # per attempt deadline
  retries: { count: 2, backoff: 10m }
  keepOnFailure: true                 # keep pod + restore for debugging
  overlapPolicy: Skip                 # Skip | Queue | Replace
status:
  lastRun: { time: ..., result: Succeeded | Failed, reason: ... }
  nextRun: ...
  conditions: []
```

### 7.2 Provider adapter interface

```go
type Provider interface {
    // Create the native restore object targeting the scratch PVC.
    Start(ctx context.Context, r *Restore, pvc *corev1.PersistentVolumeClaim) error
    // Read the native object and report phase.
    Observe(ctx context.Context, r *Restore) (Phase, string, error)
    // Remove the native object and anything it created.
    Cleanup(ctx context.Context, r *Restore) error
    // Backup finished? Used by afterBackup triggers.
    BackupsCompleted(ctx context.Context, sel BackupSelector) ([]BackupRef, error)
}
```

Adapters:

| Provider | Native restore object | Target | Done signal | Backup completion signal |
|---|---|---|---|---|
| Velero | `velero.io/v1` `Restore` | scratch PVC via namespace mapping and PV restore | `status.phase` = `Completed` / `PartiallyFailed` / `Failed` | `Backup` `status.phase` = `Completed` |
| VolSync | `volsync.backube/v1alpha1` `ReplicationDestination` (manual trigger) | VolSync-created destination PVC; scratch PVC is the copy target | `status.lastManualSync` matches trigger | `ReplicationSource` `status.lastSyncTime` changes |
| Kopiur | `kopiur.home-operations.com/v1alpha1` `Restore` (PVC `dataSourceRef`) | scratch PVC via `dataSourceRef` | Kopiur `Restore` status condition (exact names: verify in spike) | Kopiur snapshot resource completion (verify in spike) |

Exact field names and status conditions come from each tool's CRDs. The
spike (§12) pins them before the adapters are written.

### 7.3 Reconcile flow for one attempt

```
RestoreTest (cron | afterBackup | manual)
  └─ creates Restore (attempt n)
       ├─ create restored PVC(s) and scratch PVC(s) from the blueprint (owner: Restore)
       ├─ provider.Start(...)  native restore object
       ├─ wait for provider.Observe == done, or deadline (restoreWindow)
       │     deadline hit -> provider.Cleanup, delete scratch PVC, phase=Canceled
       ├─ provider done ok -> create check Pod from RestoreBlueprint
       │     pod has restored PVC mounted at restoreMount
       │     wait for pod to finish, or deadline
       ├─ pod exit 0 -> phase=Succeeded
       ├─ pod exit != 0 or Canceled
       │     attempts left -> wait backoff, next attempt (step 1)
       │     no attempts left -> phase=Failed
       └─ cleanup
             Succeeded or (Failed && !keepOnFailure): delete pod, PVC, native object
             Failed && keepOnFailure: keep pod, scratch PVC and native object (see §7.5)
```

The deadline covers the whole attempt: native restore, check pod, and
image pulls. A restore test cannot run longer than `restoreWindow` per
attempt.

Before the check pod starts, the operator must make sure no other pod holds the
restored PVC. A restored workload (for example a Velero-restored Deployment)
starts a pod on its own and holds an RWO volume. The operator scales it to 0
or deletes it first (spike, §16).

Backup and restore movers run as the database uid (see §5). When the mover uid
changes, the VolSync cache PVC must be recreated, because the old cache was
written by another uid (§16).

### 7.4 Triggers

- **Cron:** standard 5-field cron, evaluated in UTC. Each tick creates a
  `Restore` unless `overlapPolicy` blocks it (§7.6).
- **After backup:** the controller watches the provider's backup
  objects (`BackupsCompleted`). A backup that completed at time `T` starts
  a run at `T + delay`. Each backup name is recorded in status so it runs
  once.
- **Manual:** set annotation `restore.bret.linogics.io/run-now: "<nonce>"`
  on the `RestoreTest`. A new nonce starts one run. Reusing a nonce does
  nothing. Works without the CLI.

### 7.5 Keep-on-failure

When `keepOnFailure: true` and the final attempt fails, the operator keeps:
the check pod (so logs and `kubectl exec` still work), the native restore
object, and the scratch PVC. It sets a label `restore.bret.linogics.io/retained: "true"`
and an event pointing at them. Retained resources are not garbage collected
automatically; a user removes them with `kubectl delete restore`. A
`retainedFor` TTL is a possible follow-up (§13).

Pod-level `keepOnFailure` applies only to the last attempt. Earlier failed
attempts are always cleaned up, so retries do not pile up resources.

### 7.6 Overlap policy

Applies when a trigger fires while a run is in progress:

- `Skip` (default): log an event and drop the tick.
- `Queue`: record one pending run, start it when the current one ends.
  Only one queued run is kept; extra ticks are dropped.
- `Replace`: cancel the running attempt (same cleanup as a timeout), start
  a new one.

Manual triggers follow the same policy.

## 8. Scope: Namespaced or Cluster-wide (Hybrid)

**Answer to the question asked:** a hybrid is cheap if we design for it
now. It is expensive to add later, because RBAC and the controller cache
are set up at start-up.

Operator configuration:

```yaml
watchNamespaces: []      # empty = cluster-wide (default)
# watchNamespaces: [data, billing]   # namespaced mode
```

What changes with the list:

- **Controller cache:** controller-runtime `cache.Options.DefaultNamespaces`
  is set to the list when non-empty. Empty means no restriction.
- **RBAC:** a `ClusterRole` bound cluster-wide when empty. For a non-empty
  list, a `Role` + `RoleBinding` is created in each listed namespace (the
  Helm chart renders them).
- **Where pods and PVCs go:** always the namespace of the `Restore` /
  `RestoreTest`. That is inside the watched set in both modes.
- **Backup source namespace:** Velero backups live in the Velero namespace
  (`velero` by default). The operator reads them with its own
  permissions, not with the test's namespace. In namespaced mode the
  operator still needs read access to the provider's backup objects in
  the Velero namespace. This is the one cross-namespace permission and it
  is documented explicitly.

Effort estimate: small. The controller, adapters and reconcile logic do
not change. The work is the cache option, the Helm RBAC templates, and
one e2e test per mode.

Pitfall: a namespaced install with a list that misses a namespace will
silently ignore `RestoreTest` objects there. The operator logs a warning
and sets a status condition `WatchedNamespace=false` if it sees one it
does not watch.

## 9. Status and Observability

- **CR status:** phase, attempt number, last run result, next run time,
  retained resource names, conditions.
- **Events:** attempt start, provider done, check pod started, check passed,
  check failed (with exit code), window exceeded, overlap skipped, retained.
- **Prometheus metrics** (names prefixed `bret_restore_`):
  - `bret_restore_runs_total{provider,result}` (counter)
  - `bret_restore_run_duration_seconds{provider}` (histogram)
  - `bret_restore_last_success_timestamp_seconds{restoretest}` (gauge, for
    "backup not tested in N days" alerts)
  - `bret_restore_attempts_total{restoretest,reason}` (counter)

## 10. Failure Handling

| Situation | Behaviour |
|---|---|
| Native restore fails | attempt failed, retry if attempts left |
| Window exceeded before provider done | cancel native object, delete scratch PVC, attempt canceled |
| Window exceeded while check pod runs | delete pod (or keep if last attempt and `keepOnFailure`), attempt canceled |
| Check pod exit != 0 | attempt failed, retry if attempts left |
| Image pull error on check pod | treated as attempt failure, reason `ImagePull`; counts as a retry |
| Operator restarts mid-run | state is in the `Restore` status; reconcile resumes from `phase` |
| Scratch PVC cannot be bound | attempt failed after window; reason `PVCPending` |
| Mover cannot read data dir (`permission denied`) | attempt failed; reason `MoverPermission`; fix is the mover uid (§5, §16) |
| Check pod `fsGroup` changes DB data dir mode | Postgres exits at start; reason `DataDirPermissions`; blueprint must not set `fsGroup` on DB dirs |
| Restored workload still holds RWO PVC | operator scales it down before the check; no retry needed |

Every retry and every cancel writes an event with a reason. Silent
failures are a bug.

## 11. Security

- **Blueprint runs arbitrary images in the cluster.** Anyone who can create
  a `RestoreBlueprint` in a namespace can run code there with the
  restored data mounted. v1 mitigation: only admins may create
  `RestoreBlueprint` objects (RBAC in the Helm chart). Document it.
- **Blueprint and test pods do not get the operator's ServiceAccount.**
  They use a dedicated, empty ServiceAccount per namespace.
- **Restored data is sensitive.** The scratch PVC is deleted on success.
  On `keepOnFailure` the PVC is retained, so the data stays. Documented
  as a trade-off.
- **Object storage credentials** for Velero and Kopiur are read by the
  provider tools, not by the operator. The operator reads only backup
  metadata.
- No secrets in CR status or events.

## 12. Testing

### 12.1 Unit and envtest

- Adapter logic with fake provider objects.
- Reconcile state machine: every transition in §7.3, retries, window,
  overlap policy, keep-on-failure.
- Cron and after-backup scheduling with a fake clock.
- Watch-namespace logic: cache option and `WatchedNamespace` condition.

### 12.2 e2e on kind (podman provider)

Setup per test run:

1. `KIND_EXPERIMENTAL_PROVIDER=podman kind create cluster`.
2. Install Versity Gateway v1.10.0 as an in-cluster S3 endpoint with a
   posix backend and a PVC.
3. Install Velero, VolSync and Kopiur at pinned versions. Each points at
   the S3 endpoint.
4. Test flow per provider:
   - Deploy Postgres with a PVC, seed known rows.
   - Trigger a real backup with the provider.
   - Create `RestoreTest` with a `RestoreBlueprint` that runs `psql` against
     the restored data and checks row count and a seeded marker.
   - Assert `Succeeded`. Then break the backup (drop rows before backup
     or corrupt the source) and assert `Failed` with the expected reason.
   - Assert window timeout: set `restoreWindow` to a tiny value and check
     cancel + retry + cleanup.
   - Assert `keepOnFailure` retains pod, PVC and native object.
5. Namespaced mode: same flow with `watchNamespaces` set.
   Postgres live-DB spike already run for all three tools (§16). Assert objects
   outside the list are ignored and the warning condition appears.

### 12.3 Versity Gateway compatibility

Velero and Kopiur use S3 calls that a non-AWS server might not implement.
This is the first spike (§13). If Versity cannot serve a tool, the
fallback is a different S3-compatible server; the lexington repo's
choice is the default, not a guarantee.

### 12.4 CI

Open question: rootful podman in CI, or a GitHub-hosted runner with
docker. The design does not depend on which; the kind provider is set by
an environment variable.

## 13. Open Questions and Spikes

Spikes must come before the adapters are written:

1. (DONE 2026-11-14, §16) **Versity Gateway vs. Velero / VolSync / Kopiur S3 calls.** Install each
   tool against Versity and run one backup + restore. Pass/fail gates the
   Versity choice.
2. **kind on podman, rootful.** Create a cluster, install the three tools,
   run a PVC round-trip. Records what breaks on podman.
3. **Native CRD fields and status for each tool.** Pin exact `spec` and
   `status` fields for restore done, backup done, and Kopiur restore
   status (kopiur#233 included).
4. **Velero PVC-only restore.** Confirm a Velero `Restore` can be limited to
   PVC data and mapped to a scratch namespace/PVC without restoring other
   objects.

Undecided, to settle with the user:

- Final API group name (`restore.bret.linogics.io` is a placeholder).
- `retainedFor` TTL for kept resources: v1 or later.
- CI runner choice for podman (§12.4).

## 14. Calls Made Without Your Confirmation

Listed so you can check them while reading:

- Blueprint scripts are inline `containers` / `initContainers`. Scripts
  can also come from a ConfigMap via `volumes`. No separate script field.
- Cron is UTC.
- Overlap `Queue` keeps one pending run, extra ticks dropped.
- Default `overlapPolicy: Skip`.
- Operator scope: cluster-wide by default. Helm chart renders RBAC for
  both modes.
- Helm chart for install, following the lexington repo layout.
- Test tools installed at pinned versions via their Helm charts.
- Metrics prefix `bret_restore_`.
- Result values are `Succeeded` and `Failed` for both `Restore` phase and
  `RestoreTest.status.lastRun.result` (decided 2026-11-14).
- Postgres data layout `<mount>/pgdata`, `pgdata-perms` init step sets 0700.
- Scratch volumes are declared in the blueprint (`scratchVolumes`), created and
  deleted per attempt by the operator. Restored volume size defaults to source
  capacity (2026-11-14).

## 15. Build Scope

Full project. Order of work after approval:

1. Spikes (§13.1–13.4).
2. Operator SDK scaffold, CRDs, controller skeleton, cache + RBAC for
   hybrid scope.
3. Reconcile state machine with a fake provider.
4. Velero adapter, e2e.
5. VolSync adapter, e2e.
6. Kopiur adapter (alpha), e2e.
7. Metrics, events, docs, Helm chart.

## 16. Spike Findings (2026-11-14)

Verified on kind v1.37.0 (containerd 2.3.4) with csi-driver-host-path v1.19.0,
external-snapshotter v8.7.0, Versity Gateway v1.10.0 as S3.
Tools: Velero v1.18.4 (AWS plugin v1.14.4), VolSync 0.17.0, Kopiur 0.10.21 (alpha),
Postgres 17.12.

### 16.1 Backup and restore per tool

All three tools back up a live Postgres PVC and restore it into a scratch PVC.
The restored data boots and passes an SQL check (row count and marker row).
The negative check (wrong expected count) fails with exit 1.

- Velero: `Backup` phase `Completed`. Restore `Completed`, one harmless warning.
- VolSync: `ReplicationSource` `status.lastManualSync` equals trigger,
  `latestMoverStatus.result` `Successful`. Restore via `ReplicationDestination`
  with `copyMethod: Direct`.
- Kopiur: `Snapshot` `status.phase` `Succeeded`. `Restore` `status.phase` `Completed`,
  condition `Ready` reason `RestoreSucceeded`.

### 16.2 Blockers and rules

- **Velero off-cluster gap.** `Snapshot Move Data: false` keeps data in the
  cluster. The backup bucket holds metadata only. Restore worked only because the
  hostpath snapshot still existed. Rule: enable data movement for real backups.
- **Mover uid.** VolSync and Kopiur default movers cannot read the Postgres data
  dir (`permission denied`). Fix: `ReplicationSource.spec.restic.moverSecurityContext`
  `runAsUser: 999` and `fsGroup: 999`. Kopiur: `Snapshot.spec.mover.securityContext`
  `runAsUser: 999`, `runAsGroup: 999`, `runAsNonRoot: true`.
- **VolSync cache.** After a mover uid change, the old cache PVC fails with
  `permission denied` on `/cache/.../version`. Fix: delete and recreate the cache PVC.
- **Pod fsGroup breaks Postgres.** `fsGroup` changes the data dir to 2770.
  Postgres: `data directory has invalid permissions. Permissions should be u=rwx (0700)
  or u=rwx,g=rx (0750)`. Fix: no `fsGroup` on DB dirs, plus `chmod 0700` in an init step
  (the `pgdata-perms` init in the blueprints).
- **RWO workload conflict.** Velero restore starts the restored Deployment, which holds
  the PVC. The check pod cannot mount it. Fix: scale to 0 or delete first (§7.3).
- **Kopiur credentials across namespaces.** `Restore` in another namespace needs the
  repository credentials Secret copied there (`MissingCredentialsSecret` otherwise).
  Kopiur `SnapshotPolicy` repository reference is same-namespace only.
- **Crash-consistent backups work.** The DB stayed live during all backups. WAL
  recovery ran on boot and the check passed. Not a proof under heavy write load.

### 16.3 Effect on the design

- Mover uid and no `fsGroup` are operator requirements, not blueprint details.
- Blueprint `pgdata-perms` init is a convention until the operator does it itself.
- `restoredVolumes` and native sidecar are needed for data-and-DB apps (§7.1).
- Off-cluster backup needs a hard check in the operator or docs.
