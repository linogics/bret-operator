# Apps with data and a database

Some apps keep state in two places: files and a database, or a database plus a separate file
store. Both parts must come from the same backup point. This page explains why, and how to do it.

## Current limit (v1)

A `RestoreCheck` accepts one restored volume. The operator restores one PVC per `Restore`. A check
with two or more `restoredVolumes` entries is rejected with `ProviderUnsupported`.

These checks have several volumes and are not runnable by the operator in v1:
`checks/nextcloud.yaml`, `checks/wordpress.yaml`, `checks/jellyfin.yaml` and `checks/pihole.yaml`.
They run with `hack/check-test/check.sh`, which accepts several `KEY=PVC` pairs.

Not in v1:

- Restoring several volumes of one app in one attempt.
- A group key that ties several restores together.

## The problem

The timeline shows how two backups taken at different times drift apart.

```text
10:00  DB backup ────────┐
10:03  user uploads file │  row + file written
10:05  files backup ─────┤
                         ▼
restore:  file exists, no DB row     (files newer than DB)
swap the order: DB row, file missing
```

Symptoms seen in the apps:

- Nextcloud: broken file lists and "file not found" in the UI.
- WordPress: broken images (DB newer than files), or orphan uploads (DB older than files).
- Jellyfin: media items show as missing, and metadata is wrong.
- Pi-hole: lists in the DB that are not applied, or missing DNS records.
- n8n: credentials that cannot be decrypted (key mismatch, not drift).

(Source: `checks/INDEX.md`, "Apps that have data and a database".)

## Rules

1. **Restore the whole app group from one backup point.**
   - A Velero namespace backup is one point. Restoring it gives one consistent set.
   - VolSync and Kopiur work per PVC. Group the PVCs of one app with a shared trigger time,
     or accept the skew and check it.
2. **Test one volume per operator check in v1.** Use `hack/check-test` for a run that mounts
   several volumes.
3. **Quiesce the database, or rely on crash recovery.** Pick one:
   - **Crash-consistent copy, WAL recovery on boot.** Tested with Postgres 17. The copy of a
     live database recovered on boot, and the SQL check passed. Not tested under heavy write load.
   - **Pre-backup flush hook.** Flush or checkpoint the database before the snapshot. Not tested
     in this project.
   - **SQLite-in-volume apps** (gitea, grafana, home-assistant, vaultwarden, uptime-kuma): take
     the backup with the app stopped, or with a consistent snapshot. `checks/INDEX.md` names the
     SQLite file as the main risk.
4. **Write checks that cross-reference.** A DB row must point to a file that exists, and the
   file must match the row.
5. **Each restored workload holds its PVC alone.** Scale restored workloads to 0 before the
   check pod mounts the PVCs (RWO). See [best-practices.md](best-practices.md).

## Worked example: Nextcloud

Files plus Postgres, two PVCs.

- Volumes: `html` at `/var/www/html`, `pgdata` at `/var/lib/postgresql/data`.
- Files: `/var/www/html/data`. The rows that say which files exist and their sizes are in
  `oc_filecache` in Postgres.
- Restore: both PVCs from one backup selector. The operator cannot run this check in v1. Use
  `hack/check-test`.
- Repair if drift happens: `occ files:scan` can fix listings but may drop or duplicate
  entries.
- Check (`checks/nextcloud.yaml`): `occ status` shows installed, `occ integrity:check-core`
  passes, and the canary `oc_filecache.size` equals the file size on disk.
- The check runs Postgres as a native sidecar: an init container with `restartPolicy: Always`.
  The check rewrites `dbhost` to `127.0.0.1` in `config.php`.
- Open issue: `pdo_pgsql` in the image is assumed, not verified.

## Worked example: WordPress

Files plus MariaDB, two PVCs.

- Volumes: `html` at `/var/www/html`, `mysql` at `/var/lib/mysql`.
- DB rows (`wp_posts`) reference uploads in `wp-content/uploads`.
- Restore: both volumes from one backup point. Run with `hack/check-test` in v1.
- Check: MariaDB ping. The canary post's content equals the sha256 of the uploaded canary file.
- Not checked: front-end render, plugin-generated tables.

## Other apps with two volumes

- **Jellyfin:** config (`library.db`, `jellyfin.db`) and media. Restore both from one backup
  point. If media is too large, restore config only. Then the check covers the database only.
- **Pi-hole:** `gravity.db` and `/etc/dnsmasq.d`. The config must match the FTL version.
  Check the canary adlist row, the canary line in `custom.list`, and that dnsmasq config parses.
