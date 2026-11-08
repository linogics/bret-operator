# Spike findings

Verified on a kind cluster. These facts drive the rules in
[best-practices.md](best-practices.md).

## Scope of the evidence

- Verified: one Postgres 17 live database, its PVC backed up three ways, and each backup
  restored into a scratch PVC. Each restored copy booted cleanly and passed a SQL check
  (row count and a marker row).
- Not verified: heavy write load. Crash-consistent backup under load is not proven.

## Versions

| Component | Version |
|---|---|
| Velero | v1.18.4 |
| Velero AWS plugin | v1.14.4 |
| VolSync | 0.17.0 |
| Kopiur | 0.10.21 (alpha) |
| PostgreSQL | 17.12 |
| kind node image | v1.37.0 |
| csi-driver-host-path | v1.19.0 |
| external-snapshotter | v8.7.0 |

## Backup and restore

| Fact | Source tool | Notes |
|---|---|---|
| Velero, VolSync (restic, `copyMethod: Snapshot`) and Kopiur (`copyMethod: Snapshot`) all restored into a scratch PVC and booted with the SQL check. | Velero, VolSync, Kopiur | Postgres 17.12. |
| Crash-consistent backup of a live DB worked. WAL recovery runs on boot. | Postgres | Not proof under heavy write load. |
| Velero CSI backup without `--snapshot-move-data` does **not** put volume data off-cluster. Restore only worked because the snapshot still existed in the cluster. Off-cluster recovery needs data movement enabled. | Velero v1.18.4 | Hard rule. |
| Velero restore warning `ConfigMap:kube-root-ca.crt already exists` is harmless. | Velero v1.18.4 | |
| Velero restore auto-starts a restored Deployment that holds the PVC. An RWO PVC mounts only once, so the check pod needs the PVC free. | Velero v1.18.4 | Scale restored workloads to 0 or delete them first. |
| Kopiur `Restore` status phase is `Completed`, not `Succeeded`. | Kopiur 0.10.21 | |
| Kopiur `Restore` condition `Ready` has reason `RestoreSucceeded`. | Kopiur 0.10.21 | |
| Kopiur `Restore` in another namespace needs the repo credentials Secret copied into that namespace. | Kopiur 0.10.21 | |

## Permissions and mover pods

| Fact | Source tool | Exact text |
|---|---|---|
| Mover pods (VolSync, Kopiur) default to a non-DB UID and cannot read a postgres data dir. | VolSync 0.17.0, Kopiur 0.10.21 | `permission denied` |
| Fix: set the mover security context to the DB UID. 999 for `postgres:17`. | VolSync, Kopiur | |
| Pod `fsGroup` on the restored data dir changes it to `2770`. Postgres refuses to start. | Postgres 17.12 | `Permissions should be u=rwx (0700) or u=rwx,g=rx (0750)` |
| Fix: no `fsGroup` on DB data dirs, or `chmod 0700` in an init step. | Postgres 17.12 | |
| VolSync cache PVCs created by a previous mover UID cause `permission denied`. | VolSync 0.17.0 | Recreate the cache PVC when the mover UID changes. |

## Check behaviour

| Fact | Notes |
|---|---|
| A negative check (expected count set wrong) fails with exit 1. | The check can fail. Use this as a test of every check. |

## Pitfalls summary

1. Off-cluster backups need data movement. Without it, the restore depends on the
   in-cluster snapshot.
2. Mover UID must match the DB UID (999 for Postgres 17).
3. No `fsGroup` on a database data directory.
4. Restored workloads hold RWO PVCs. Scale them to 0 before the check.
5. Recreate VolSync cache PVCs when the mover UID changes.
6. Kopiur cross-namespace restores need the repo credentials Secret copied.
7. Kopiur phase is `Completed`. Its condition reason is `RestoreSucceeded`.

## Not covered by the spike

- Heavy write load and crash consistency under load.
- Pre-backup flush hooks.
- Velero PVC-only restore mapping (design §13.4).
- Versity Gateway as S3 backend (design §13.1). Spike facts do not record it.
- Kopiur backup-completion signal and the kopiur#233 restore issue.
- Postgres 16.4 was never tested. `checks/postgres.yaml` now uses 17.12, the version the spike verified.
