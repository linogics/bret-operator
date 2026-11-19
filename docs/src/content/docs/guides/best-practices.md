---
title: Best practices
description: Rules for backups and restore tests.
---

Rules for backups and restore tests.

## Restore target

- Restore into a scratch PVC only. Never restore over live data.
- A restore test must not write to a PVC that the application uses.
- Restored data is sensitive. The operator deletes scratch PVCs when an attempt ends. With
  `keepOnFailure: true`, it keeps them after the final failed attempt so you can debug.

## Backups

- Back up off-cluster. A Velero CSI backup needs `--snapshot-move-data`. Without it, the volume
  data stays in the cluster as a snapshot, and a restore works only while that snapshot exists.
- A crash-consistent backup of a live database is useful, but it is not proof of recovery. The
  tested Postgres 17 backup recovered on boot and passed the SQL check. The load was light. Test
  under heavy write load before you rely on it.
- Seed the canary before the backup. The check depends on it. See [Write a check](/guides/write-a-check/#canary-convention).

## Restore tests

- Run restore tests on a schedule and after each backup. Use `triggers.cron` and
  `triggers.afterBackup`.
- Test that the check can fail. Set the expected value wrong and confirm exit code 1. A check
  that cannot fail proves nothing.
- Never report a pass without running the check.
- Set `restoreWindow` on every test. It is the deadline for each attempt.
- Keep failures for debugging with `keepOnFailure: true`. Only the last failed attempt is kept.
- Keep retries small. Use `retries.count` of 1 or 2, with a backoff. The operator deletes earlier
  failed attempts, so retries do not pile up.
- Do not delete evidence silently. Kept objects stay until someone deletes them, or deletes the
  `Restore`.
- Watch Events for skipped and replaced runs, and for canceled attempts.
- Alert on the metric `bret_restoretest_last_success_timestamp_seconds`. Use it for
  "backup not tested in N days".

## Restore mechanics

- **Mover pods need the database UID.** VolSync and Kopiur mover pods default to a non-database
  UID. They cannot read a Postgres data directory and fail with `permission denied`. Set the
  mover security context to the database UID. For `postgres:17`, that UID is **999**.
- **Do not set `fsGroup` on database data directories.** `fsGroup` changes the data directory to
  `2770`. Postgres then refuses to start:
  `Permissions should be u=rwx (0700) or u=rwx,g=rx (0750)`.
  Either set no `fsGroup`, or run `chmod 0700` on the data directory in an init container.
- **Use one restored workload per RWO PVC.** RWO PVCs mount once. A Velero restore starts any
  restored Deployment automatically, and that Deployment holds the PVC. Scale restored workloads
  to 0, or delete them, before the check pod runs.
- **Recreate the VolSync cache PVC when the mover UID changes.** A cache PVC made by an older
  mover UID causes `permission denied`.
- **Copy the repository credentials Secret into the namespace of a Kopiur `Restore`.** The
  `Restore` does not read it from another namespace.
- **Expect Kopiur phase `Completed`, not `Succeeded`.** The Kopiur `Restore` condition `Ready`
  has reason `RestoreSucceeded`.
- **Ignore the Velero warning `ConfigMap:kube-root-ca.crt already exists`.** It is harmless.

## Security

- Use least-privilege RBAC. Give the operator only the verbs and namespaces it needs. In
  namespaced mode, the one cross-namespace read is the provider's backup objects in the Velero
  namespace.
- Only admins create `RestoreCheck` objects. A check runs arbitrary images in the cluster with
  the restored data mounted. Treat it as trusted code.
- Check pods run as the `serviceAccountName` set in the `RestoreCheck`, or as the namespace
  default when it is unset. Give that account no RBAC rights.
- Do not put secrets in CR status or Events.
- The provider tools read object storage credentials, not the operator. The operator reads
  backup metadata only.
- Pin tool and image versions. Use pinned image tags in checks. Check tags marked unverified in
  [Check catalog](/guides/check-catalog/) need a test run before use.

## Data apps

- Restore all parts of one app from **one backup point**. Files and the database must come from
  the same point. See [Data and database](/guides/data-and-database/).
- Check that the parts point at each other. A DB row must point to a file that exists.
