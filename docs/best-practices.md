# Best practices

Rules for backups and restore tests. Each rule names its source: **Spike** (verified on a
kind cluster), **Design** (design doc section), or **Index** (`blueprints/INDEX.md`).

## Restore target

- **Scratch PVC only. Never restore over live data.** (Design §4, §6.4.)
- A restore test must not write to a PVC the application uses. (Design §4.)
- Restored data is sensitive. Scratch PVCs are deleted on success. With `keepOnFailure`
  they are kept, so the data stays. This is a trade-off. (Design §11.)

## Backups

- **Back up off-cluster. Velero CSI backups need data movement.** A Velero CSI backup
  without `--snapshot-move-data` does not put volume data off-cluster. The restore worked
  in the spike only because the snapshot still existed in the cluster. Off-cluster recovery
  needs data movement enabled. (Spike.)
- **Crash-consistent backups of a live database are useful but not proof.** The spike
  backed up a live Postgres 17 database and restored it. WAL recovery ran on boot and the
  SQL check passed. This was not tested under heavy write load. (Spike.)
- Seed a canary before the backup. The check depends on it. (Index.)

## Restore tests

- **Run restore tests on a schedule and after each backup.** Use `cron` and `afterBackup`
  triggers. (Design §7.4.)
- **Test that the check can fail.** Set the expected value wrong. The spike did this, and
  the check exited 1. A check that cannot fail proves nothing. (Spike.)
- **Never pass a test without running the check.** (Design §4.)
- **Every test has a deadline.** Set `restoreWindow`. (Design §4, §7.3.)
- **Keep failures for debugging.** Use `keepOnFailure: true`. Only the last attempt is kept.
  (Design §7.5.)
- **Keep retries small.** Use `retries.count` of 1 or 2, with a backoff. Earlier failed
  attempts are cleaned up, so retries do not pile up. (Design §7.1, §7.5.)
- **Do not delete evidence silently.** Failed tests keep their resources until a user
  deletes them. (Design §4.)
- **Write an Event for every retry and cancel, with a reason.** (Design §10.)
- **Alert on the metric `bret_restore_last_success_timestamp_seconds`.** Use it for
  "backup not tested in N days". (Design §9.)

## Restore mechanics

- **Mover pods need the database UID.** VolSync and Kopiur mover pods default to a non-DB
  UID. They cannot read a Postgres data directory and fail with `permission denied`. Set the
  mover security context to the DB UID. For `postgres:17` that is **999**. (Spike.)
- **No `fsGroup` on database data directories.** `fsGroup` changes the data directory to
  `2770`. Postgres then refuses to start:
  `Permissions should be u=rwx (0700) or u=rwx,g=rx (0750)`.
  Either set no `fsGroup`, or run `chmod 0700` on the data dir in an init step. (Spike.)
- **One restored workload per RWO PVC.** RWO PVCs mount once. A Velero restore auto-starts
  any restored Deployment, and that Deployment holds the PVC. Scale restored workloads to 0,
  or delete them, before the check pod runs. (Spike.)
- **Recreate the VolSync cache PVC when the mover UID changes.** A cache PVC made by an
  older mover UID causes `permission denied`. (Spike.)
- **Copy the repo credentials Secret into the namespace of a Kopiur Restore.** A Kopiur
  `Restore` in another namespace needs the Secret there. (Spike.)
- **Expect Kopiur phase `Completed`, not `Succeeded`.** Kopiur `Restore` condition `Ready`
  has reason `RestoreSucceeded`. (Spike.)
- **The Velero warning `ConfigMap:kube-root-ca.crt already exists` is harmless.** (Spike.)

## Security

- **Least-privilege RBAC.** Give the operator only the verbs and namespaces it needs. In
  namespaced mode, the one cross-namespace read is the provider's backup objects in the
  Velero namespace. Document it. (Design §8.)
- **Only admins create `RestoreBlueprint` objects.** A blueprint runs arbitrary images in
  the cluster with the restored data mounted. Treat it as trusted code. (Design §11.)
- **Blueprint and test pods do not use the operator's ServiceAccount.** They use a
  dedicated, empty ServiceAccount per namespace. (Design §11.)
- **No secrets in CR status or Events.** (Design §11.)
- **Object storage credentials are read by the provider tools, not the operator.** The
  operator reads only backup metadata. (Design §11.)
- **Pin tool and image versions.** Velero, VolSync and Kopiur are pinned in tests. Use
  pinned image tags in blueprints. Some blueprint tags are unverified. (Design §5, §12.2;
  Index.)
- Kopiur is alpha. Its restore has open issue kopiur#233. Treat its results with care. (Design §5.)

## Data apps

- Restore all parts of one app from **one backup point**. Files and the database
  must come from the same point. See [data-and-database.md](data-and-database.md).
- Check that the parts point at each other. A DB row must point to a file that exists.
  (Index.)
