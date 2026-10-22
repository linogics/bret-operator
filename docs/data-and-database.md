# Apps with data and a database

Some apps keep state in two places: files and a database, or a database plus a separate
file store. Both must come from the same backup point. This page explains why, and how to
do it.

Design status: restoring several volumes together needs `restoredVolumes` and a group key.
Both are **design deltas, proposed**. Neither is in design §7.1 or the Go types.

## The problem

If the database comes from 10:00 and the files come from 10:05, they drift apart:

- DB rows point at files that are missing.
- Files exist with no DB row for them.

Symptoms seen in the apps:

- Nextcloud: broken file lists and "file not found" in the UI.
- WordPress: broken images (DB newer than files), or orphan uploads (DB older than files).
- Jellyfin: media items show as missing, and metadata is wrong.
- Pi-hole: lists in the DB that are not applied, or missing DNS records.
- n8n: credentials that cannot be decrypted (key mismatch, not drift).

(Source: `blueprints/INDEX.md`, "Apps that have data and a database".)

## Rules

1. **Restore the whole app group from one backup point.**
   - A Velero namespace backup is one point. Restoring it gives one consistent set.
   - VolSync and Kopiur work per PVC. Group the PVCs of one app with a shared trigger time,
     or accept skew and check it.
   - The group key that ties several restores together is **design delta, proposed**.
     Not specified yet.
2. **Use `restoredVolumes` for multi-PVC apps.** Each volume is its own PVC, restored from
   the same point. Mount each one at its original path. (Design delta, proposed.)
3. **Quiesce the database, or rely on crash recovery.** Pick one:
   - **Crash-consistent copy, WAL recovery on boot.** The spike did this with Postgres 17.
     The copy of a live database was recovered on boot, and the SQL check passed. This is
     not proof under heavy write load.
   - **Pre-backup flush hook.** Flush or checkpoint the DB before the snapshot. Not
     covered by the spike. **Unverified** as a pattern in this project.
   - For SQLite-in-volume apps (gitea, grafana, home-assistant, vaultwarden, uptime-kuma):
     take the backup with the app stopped, or with a consistent snapshot. The
     `INDEX.md` says the sqlite file itself is the main risk.
4. **Write checks that cross-reference.** A DB row must point to a file that exists, and
   the file must match the row.
5. **Each restored workload holds its PVC alone.** Scale restored workloads to 0 before the
   check pod mounts the PVCs (RWO). See [best-practices.md](best-practices.md).

## Worked example: Nextcloud

Files plus Postgres, two PVCs.

- Volumes: `html` at `/var/www/html`, `pgdata` at `/var/lib/postgresql/data`.
- Files: `/var/www/html/data`. The rows that say which files exist and their sizes:
  `oc_filecache` in Postgres.
- Restore: both PVCs in one attempt, from one backup selector.
- Repair if drift happens: `occ files:scan` can fix listings but may drop or duplicate
  entries. (Index.)
- Check (`blueprints/nextcloud.yaml`): `occ status` shows installed, `occ integrity:check-core`
  passes, and the canary `oc_filecache.size` equals the file size on disk.
- Blueprint uses a native sidecar for Postgres (design delta). The check rewrites
  `dbhost` to `127.0.0.1` in `config.php`.
- Open issue: `pdo_pgsql` in the image is assumed, not verified.

## Worked example: WordPress

Files plus MariaDB, two PVCs.

- Volumes: `html` at `/var/www/html`, `mysql` at `/var/lib/mysql`.
- DB rows (`wp_posts`) reference uploads in `wp-content/uploads`.
- Restore: both volumes in one attempt.
- Check: MariaDB ping, and the canary post's content equals the sha256 of the uploaded
  canary file.
- Not checked: front-end render, plugin-generated tables. (Index.)

## Other apps with two volumes

- **Jellyfin:** config (`library.db`, `jellyfin.db`) and media. Restore both from one
  attempt. Option: restore config only if media is too large. Then the check covers the DB
  only.
- **Pi-hole:** `gravity.db` and `/etc/dnsmasq.d`. The config must match the FTL version.
  Check the canary adlist row, the canary line in `custom.list`, and that dnsmasq config parses.

## Design deltas in this page

| Item | State |
|---|---|
| `restoredVolumes` in `RestoreBlueprint` | design delta, proposed |
| Group key tying several `Restore` objects to one backup point | design delta, proposed, not specified |
| Native-sidecar init containers | design delta, needs a decision |
