# Apps with data and a database

Some apps keep state in two places: files and a database, or a database plus a separate file
store. Both parts must come from the same backup point. This page explains why, and how to do it.

## Several volumes

A `RestoreCheck` lists every volume in `restoredVolumes`. A `Restore` or `RestoreTest` restores
one source per volume in one attempt, and the check pod mounts them all. The `sourcePVC` values tie
the two together. See [concepts.md](concepts.md#which-pvc-gets-restored).

The checks for apps with several volumes are `checks/nextcloud.yaml`, `checks/wordpress.yaml`,
`checks/jellyfin.yaml` and `checks/pihole.yaml`.

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
   - VolSync and Kopiur work per PVC. List every PVC of the app as a source of one `Restore` or
     `RestoreTest`. Take the backups at the same time, or accept the skew and check it. A
     `RestoreTest` with `afterBackup` waits until every source has a new backup.
2. **Restore all volumes of the app in one attempt.** One source per PVC, one check that mounts them all.
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
- Restore: two sources, `html` and `pgdata`, in one `Restore` or `RestoreTest`. Example:
  [examples/kopiur/restoretest.yaml](../examples/kopiur/restoretest.yaml).
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
- Restore: two sources, `html` and `mysql`, in one attempt.
- Check: MariaDB ping. The canary post's content equals the sha256 of the uploaded canary file.
- Not checked: front-end render, plugin-generated tables.

## Other apps with two volumes

- **Jellyfin:** config (`library.db`, `jellyfin.db`) and media. Restore both from one backup
  point. If media is too large, restore config only. Then the check covers the database only.
- **Pi-hole:** `gravity.db` and `/etc/dnsmasq.d`. The config must match the FTL version.
  Check the canary adlist row, the canary line in `custom.list`, and that dnsmasq config parses.
