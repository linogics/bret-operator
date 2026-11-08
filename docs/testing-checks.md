# Testing checks

Test a check before you trust it. A check that was never run can pass for the wrong reason, or
fail for a reason that has nothing to do with the backup.

This page covers the harness in `hack/check-test/`. It works without the operator's
controllers, so you can test today.

## What the harness does

| Script | Purpose |
|---|---|
| `render.py` | Turns a `RestoreCheck` into a check pod, the way the operator is meant to. Mounts restored volumes at their paths, creates scratch PVCs. |
| `check.sh` | Runs one check pod against restored PVCs you name. Prints each container's exit code and the last logs. Exit 0 means pass. |
| `roundtrip.sh` | Full test for one app: seed a source volume, back it up with Kopiur, restore it into scratch PVCs, run the check. |
| `seeds/<app>.yaml` | The seed for one app: source PVCs and a `seed` pod that writes the data and the canary. |

## Prerequisites

- A Kubernetes cluster you can change. Do not run the harness against production.
- `kubectl`, `python3` with PyYAML.
- Kopiur installed, and an S3-compatible endpoint for its repository. The harness uses the
  endpoint and credentials written in `roundtrip.sh`. Change them for your environment.
- A `VolumeSnapshotClass` named `csi-hostpath-snapclass` and a `StorageClass` named
  `csi-hostpath-sc`. Change the names in `roundtrip.sh` and the seeds if yours differ.
- The spike cluster used kind on nerdctl, csi-driver-host-path and Versity Gateway. See
  [development.md](development.md) for the local setup.

## Run one check against existing PVCs

Use this when you already have restored PVCs and only want to run the check:

```bash
hack/check-test/check.sh checks/postgres.yaml bpt-postgres check-postgres data=r-data
```

Arguments:

1. the check file,
2. the namespace the pod runs in (created if missing),
3. the pod name,
4. one `KEY=PVC` pair per restored volume.

The KEY is `data` for a `restoreMount` check. For `restoredVolumes`, the KEY is the entry
name. List the keys with:

```bash
python3 hack/check-test/render.py checks/nextcloud.yaml --list
```

Environment variables:

| Variable | Default | Meaning |
|---|---|---|
| `TIMEOUT` | `900` | Seconds to wait for the pod to finish. |
| `KEEP` | unset | Set to `1` to keep the pod after the run. |

Output ends with `VERDICT: PASS` or `VERDICT: FAIL`. The script exits 0 on pass.

## Run the full round trip for one app

```bash
hack/check-test/roundtrip.sh postgres
```

This runs these steps in namespace `bpt-postgres`:

1. Create a Kopiur `Repository` and a `SnapshotPolicy` for each volume the check needs.
2. Apply `hack/check-test/seeds/postgres.yaml`. Wait for the `seed` pod to succeed.
3. For each volume: create a `Snapshot`, wait for `Succeeded`, create a `Restore` and a PVC
   named `r-<volume>`, wait for `Completed` and `Bound`.
4. Run the check against the `r-<volume>` PVCs.
5. Delete the namespace, unless `KEEP=1`.

The exit code of `roundtrip.sh` is the exit code of the check.

The mover UID for the restore and the source snapshot comes from the check's first
`runAsUser`. That is why every check must set `runAsUser` on its containers.

Environment variables: `TIMEOUT` (per wait, default 900 seconds) and `KEEP`.

Custom paths:

```bash
hack/check-test/roundtrip.sh minimal \
  hack/check-test/seeds/minimal.yaml \
  examples/restorecheck-minimal.yaml
```

## Writing a seed

A seed is the only per-app work. It must satisfy this contract:

- One `PersistentVolumeClaim` per volume key. `restoreMount` checks use the key `data`.
  `restoredVolumes` checks use the entry names. The PVC names match the keys exactly.
- One `Pod` named `seed` that writes the data and the canary, then exits 0. The harness
  waits for `Succeeded`.
- A crash-consistent end state is good. The seed may kill the database hard, as the Postgres
  seed does. The check then has to recover, which is part of the test.
- Use the same UID the check uses. If the PVC starts owned by root, add a root init
  container that runs `chown <uid>:<gid>` on the mount, as the seeds do.

Start from `seeds/postgres.yaml` or `seeds/minimal.yaml`.

## Negative test

A check that can never fail proves nothing. After a pass, break the expected value (for example
change the canary payload's hash in the seed) and run the round trip again. The check must
report `VERDICT: FAIL`. The spike did this with a wrong expected count.

## Reading a failure

`check.sh` prints the exit code of every init and check container, then the last 40 log lines
of each. Common cases:

| Symptom | Likely cause |
|---|---|
| `init ... exit=` non-zero, `permission denied` in the init log | UID mismatch or a Postgres-style data dir mode. See [checks.md](checks.md#pod-security). |
| Pod stays `Pending` with `persistentvolumeclaim ... not found` | Restored PVC not created or wrong claim name. Check the `KEY=PVC` pairs. |
| `pg_amcheck: no relations to check` | The extension is not installed in the scratch copy. The Postgres check now creates `amcheck` first. |
| `another server might be running; trying to start server anyway` | Harmless. A crash-consistent copy has a stale `postmaster.pid`. |
| Restore never reaches `Completed` | Read the Kopiur `Restore` conditions and the mover pod logs. See [troubleshooting.md](troubleshooting.md). |

## Images

`roundtrip.sh` and `check.sh` pull images from the registry. Check the tag exists before you
rely on a check:

```bash
docker manifest inspect postgres:17.12 >/dev/null && echo ok
# or
nerdctl --namespace k8s.io pull postgres:17.12
```

## Status of the app checks

See [checks/INDEX.md](../checks/INDEX.md). It records, per app, whether the round trip
passed, the negative test result, and the open issues.
