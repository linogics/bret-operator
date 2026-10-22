# CRD reference

Group: `restore.bret.linogics.io/v1alpha1` (placeholder name, final name is open, design §13).

Status: the Go types in `api/v1alpha1/` are scaffolds. `Restore`, `RestoreTest` and
`RestoreBlueprint` currently hold only a placeholder `Foo` field. Every field below
is **planned** unless it says otherwise. Examples are copied from design §7.1.

Read the field tables as the design, not as what the cluster accepts today.

---

## Restore

One restore run. Created by hand, or by a `RestoreTest`.

### Example

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

### Spec fields

| Name | Type | Required | Default | Meaning | State |
|---|---|---|---|---|---|
| `spec.provider` | string: `velero`, `volsync`, `kopiur` | yes | none | Which tool does the restore. | planned |
| `spec.source.backupRef.name` | string | yes | none | Backup to restore. | planned |
| `spec.source.backupRef.namespace` | string | unverified | none | Namespace of the backup object. Comment says "provider-specific". | planned |
| `spec.target.storageClassName` | string | unverified | none | Storage class of the scratch PVC. | planned |
| `spec.target.size` | quantity | unverified | none | Size of the scratch PVC. | planned |

Required and default columns marked "unverified" are not stated in the design.

### Status fields

| Name | Type | Meaning | State |
|---|---|---|---|
| `status.phase` | string: `Pending`, `Running`, `Succeeded`, `Failed`, `Canceled` | Overall state of the run. | planned |
| `status.attempt` | int | Current attempt number. | planned |
| `status.conditions` | list | Standard Kubernetes conditions. Types not yet specified. | planned |
| `status.providerRef` | `{kind, name, namespace}` | The native object the operator created. | planned |

### Conditions

Not yet specified. The only condition named in the design is `WatchedNamespace`
on `RestoreTest` (see below). The Kopiur native `Restore` has its own `Ready`
condition with reason `RestoreSucceeded`. That belongs to Kopiur, not to this CRD.

---

## RestoreTest

Schedule and policy. Creates `Restore` objects.

### Example

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

### Spec fields

| Name | Type | Required | Default | Meaning | State |
|---|---|---|---|---|---|
| `spec.provider` | string: `velero`, `volsync`, `kopiur` | yes | none | Tool that makes the backups and restores. | planned |
| `spec.backupSelector.labels` | map | one of selector or `backupRef` | none | Picks backups by label. | planned |
| `spec.backupRef` | explicit backup reference | one of selector or `backupRef` | none | Shape not given in the design. | **Not yet specified** |
| `spec.blueprintRef.name` | string | yes | none | `RestoreBlueprint` to run as the check. | planned |
| `spec.triggers.cron` | string, 5-field cron | no | none | Schedule, UTC. | planned |
| `spec.triggers.afterBackup.delay` | duration | no | none | Wait after a backup completes. | planned |
| `spec.triggers.manual` | bool | no | none | Enables the run-now annotation. | planned |
| `spec.restoreWindow` | duration | yes (design example) | not stated | Deadline per attempt. | planned |
| `spec.retries.count` | int | no | not stated | Extra attempts. | planned |
| `spec.retries.backoff` | duration | no | not stated | Wait between attempts. | planned |
| `spec.keepOnFailure` | bool | no | not stated | Keep pod, PVC, native object after last failed attempt. | planned |
| `spec.overlapPolicy` | enum: `Skip`, `Queue`, `Replace` | no | `Skip` (design §14) | What to do when a trigger fires during a run. | planned |

### Status fields

| Name | Type | Meaning | State |
|---|---|---|---|
| `status.lastRun.time` | time | When the last run ended. | planned |
| `status.lastRun.result` | `Succeeded` or `Failed` | Result of the last run. | planned |
| `status.lastRun.reason` | string | Why, e.g. `ImagePull`, `PVCPending`. | planned |
| `status.nextRun` | time | Next cron tick. | planned |
| `status.conditions` | list | Includes `WatchedNamespace` (see below). | planned |

Not yet specified: the status field that records which backup names already ran
(needed for `afterBackup` "runs once"), and the field that lists retained resource
names (design §9).

Note: `Restore` phases and `RestoreTest.status.lastRun.result` both use `Succeeded` and
`Failed` (decided 2026-11-14).

### Conditions

| Type | Status | Meaning | State |
|---|---|---|---|
| `WatchedNamespace` | `False` | This `RestoreTest` is in a namespace the operator does not watch. Only set in namespaced mode. | planned (design §8) |

---

## RestoreBlueprint

The check. A `PodSpec`-shaped template. The operator adds the restored volume to the pod
and mounts it at `restoreMount` in every container. The exit code of each container is the
verdict (see [blueprints.md](blueprints.md)).

### Example

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

### Spec fields

| Name | Type | Required | Default | Meaning | State |
|---|---|---|---|---|---|
| `spec.restoreMount` | path | yes, for single-PVC blueprints | none | Mount path of the restored volume in every container. | planned |
| `spec.initContainers` | list of containers | no | `[]` | Run before the check containers. | planned |
| `spec.containers` | list of containers | yes | none | The checks. Exit 0 = pass. | planned |
| `spec.volumes` | list of volumes | no | `[]` | Extra volumes, e.g. ConfigMap or Secret. | planned |
| `spec.restoredVolumes` | list of `{name, mountPath}` | for multi-PVC apps | none | Several restored volumes, each with its own mount path. | **design delta, proposed**. Not in design §7.1. Used in `blueprints/nextcloud.yaml`, `wordpress.yaml`, `jellyfin.yaml`, `pihole.yaml`. |

### restoredVolumes (design delta, proposed)

Proposed shape, as used in `blueprints/nextcloud.yaml`:

```yaml
spec:
  restoredVolumes:
  - name: html
    mountPath: /var/www/html
  - name: pgdata
    mountPath: /var/lib/postgresql/data
```

Each entry is its own PVC, restored from the same backup point. Open point: how the
operator picks the one backup point for all of them. See
[data-and-database.md](data-and-database.md).

### Init container fields used by the blueprints (design delta, proposed)

The blueprints use these fields on an init container. The design §7.1 says init
containers run before the checks and exit. These fields make an init container a
long-running native sidecar. They need Kubernetes 1.29 or later.

| Field | Used in | Meaning |
|---|---|---|
| `restartPolicy: Always` | `nextcloud.yaml`; `INDEX.md` also lists keycloak and wordpress (mariadb) | Makes the init container a native sidecar that keeps running. |
| `startupProbe` | `nextcloud.yaml` | Gate before checks start. |

Source: `blueprints/INDEX.md`, "Native sidecars". Needs a decision in the design.

### Status fields

`RestoreBlueprint` status: not specified. The Go type has an empty status struct.

### Conditions

Not specified.

---

## Not yet specified

Items the docs cannot fill from the design or the Go types. Do not guess these.

- `spec.backupRef` shape on `RestoreTest` (design says "or explicit backupRef").
- Default values for `keepOnFailure`, `retries.count`, `retries.backoff`, `restoreWindow`.
- Required flags for `Restore.spec.source.backupRef.namespace`, `target.storageClassName`, `target.size`.
- Condition types other than `WatchedNamespace`.
- Status field that records handled backup names (`afterBackup` runs once).
- Status field that lists retained resource names.
- `retainedFor` TTL (design §13, open).
- Group key that ties several `Restore` objects to one backup point (needed for multi-PVC apps).
- Kopiur backup-completion signal (design §7.2 says "verify in spike"; the spike facts do not cover it).
- Velero restore field mapping for PVC-only restore (design §13.4, spike not recorded).
- Native-sidecar init container semantics (see above).
- Final API group name.
