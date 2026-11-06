# Size rule, providers and API group: design

Date: 2026-11-15
Status: approved

## Intent

Restores need a scratch volume of the right size. Users should not have to guess it. The operator derives the size from the backup source where a durable source exists. It fails early, with a clear condition, when it cannot.

## Goals

- One clear size rule per provider.
- Velero and VolSync in v1 next to Kopiur.
- Helm chart enables only the providers in use. RBAC follows the enabled providers.
- Final API group `bret.linogics.io`.

## Non-goals

- RestoreTest real check on the cluster. Done after the features below.
- Postgres 16.x testing. Accepted untested.
- Velero data-movement size (DataUpload/PodVolumeBackup). Not needed: the rule uses the PVC request from the backup.

## Decisions

### API group

- Group: `bret.linogics.io`, version `v1alpha1`.
- Kinds: `Restore` (plural `restores`), `RestoreTest` (plural `restoretests`), `RestoreBlueprint` (plural `restoreblueprints`).
- Labels and annotations move with the group, for example `bret.linogics.io/retained` and `bret.linogics.io/mover-uid`.

### Size rule

`target.size` is optional.

Source size, per provider:

| Provider | Source size | Durable? |
|---|---|---|
| Kopiur | Snapshot `status.stats.sizeBytes` (data size) | yes |
| Velero | PVC `spec.resources.requests.storage` in backup contents | yes |
| VolSync | `spec.resources.requests.storage` of the source PVC named in ReplicationSource `spec.sourcePVC` | only while the PVC exists |

If the source size is known, the operator uses `autoExpand` to compute the size.

If the source size is unknown (VolSync PVC missing, Velero contents unreadable), `target.size` must be set. Otherwise the Restore fails with condition `SizeUnknown`.

If `target.size` is set and is smaller than the source size, the Restore fails with condition `InsufficientCapacity`.

### autoExpand

```yaml
autoExpand:
  percentage: 10   # % of source size, must be > 0
  min: 1Gi         # lower bound
  max: 5Gi         # upper bound, optional
```

Rule: `size = clamp(sourceSize × percentage / 100, min, max)`.

- `min` must not be greater than `max` (CEL).
- Fixed size idiom: `percentage: 100, min: 5Gi, max: 5Gi`.
- Default when `target.size` and `autoExpand` are both omitted: `percentage: 10, min: 1Gi`.

Caveat: a live PVC (VolSync) shows its size now, not at backup time. If the app resized it after the backup, the derived size can be wrong. The docs say so.

### Provider RBAC and controllers

Chart flags: `providers.kopiur.enabled`, `providers.velero.enabled`, `providers.volsync.enabled` (booleans); `providers.velero.namespace` is the Velero install namespace.

- Flag off: no RBAC rules for that API group, and its controller does not start.
- VolSync needs `get` on PVCs in the namespaces it reads. That is a Role per watched namespace.

### watchNamespaces

- Chart value `watchNamespaces`, a list.
- Empty list: cluster-wide.
- Non-empty: the manager cache and the RBAC Roles are limited to those namespaces.

### handledBackups (RestoreTest trigger)

- Replace the list with a watermark: name and creation time of the last handled backup.
- Backups newer than the watermark trigger a run. Older ones do not.

### Volume mapping

- `restoredVolumes[]` gets a `sourcePVC` field, the PVC name in the source namespace.
- The volume name in the blueprint is the key. Pods reference it, and it does not depend on the PVC name.
- CEL: `sourcePVC` required for `restoredVolumes`. `restoreMount` stays for the single-volume case.

### Scratch volume lifetime

- The scratch PVC lives for the RestoreTest attempt.
- On success it is deleted.
- On failure with `keepOnFailure: true` it stays for the next retry, then is deleted when the RestoreTest finishes.
- No TTL field in v1.

### Overlap Replace

Replace leaves the old Restore for garbage collection. Documented.

### Velero in v1

- `Restore` for the PVC and PV subset: `includedResources: [persistentvolumeclaims, persistentvolumes]`, `namespaceMapping: {<source ns>: bret-scratch-<restore>}`, `restorePVs: true`.
- Velero creates the target namespace. The operator needs no namespace RBAC.
- Open risk, for a spike before merge: PV name clashes with live PVs.

## Approaches considered

- Single size rule for all providers: rejected. No provider has a durable structured size for all three.
- Size from rsync log text (VolSync): rejected. Text output, not an API.
- TTL on kept objects: rejected for v1. Tied to the RestoreTest attempt instead.

## Left to the builder

- Exact condition types and reasons.
- Exact CEL expressions.
- Velero contents read path: a DownloadRequest for BackupContents, per the spike. Failure falls back to requiring `target.size`.
