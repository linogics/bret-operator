---
title: Test a check by hand
description: Run a check against restored data before the operator does it.
---

Test a check before you trust it. A check that was never run can pass for the wrong reason, or
fail for a reason that has nothing to do with the backup.

This page covers the harness in `hack/check-test/`. It works without the operator's
controllers, so you can test today.

## What the harness does

| Script | Purpose |
|---|---|
| `render.py` | Turns a `RestoreCheck` into a check pod. It runs the Go renderer in `hack/check-test/render`, which calls the operator's own code, so the pod is the same one the operator makes. |
| `check.sh` | Runs one check pod against restored PVCs you name. Prints each container's exit code and the last logs. Exit 0 means pass. |
| `roundtrip.sh` | Full test for one app: seed a source volume, back it up with Kopiur, restore it into `r-<volume>` PVCs, run the check. |
| `checks/<app>/seed.yaml` | The seed for one app: source PVCs and a `seed` pod that writes the data and the canary. |

## Prerequisites

- A Kubernetes cluster that you can change. Do not run the harness against production.
- `kubectl`, `python3` with PyYAML.
- Kopiur installed, and an S3-compatible endpoint for its repository. The harness uses the
  endpoint and credentials written in `roundtrip.sh`. Change them for your environment.
- A `VolumeSnapshotClass` named `csi-hostpath-snapclass` and a `StorageClass` named
  `csi-hostpath-sc`. Change the names in `roundtrip.sh` and the seeds if yours differ.
- The test cluster setup is described in
  [development.md](/contributing/development/) for the local setup.

## Run one check against existing PVCs

Use this when you already have restored PVCs and only want to run the check:

```bash
hack/check-test/check.sh checks/nextcloud/restorecheck.yaml bpt-nextcloud check-nextcloud html=r-html pgdata=r-pgdata
```

Arguments:

1. the check file,
2. the namespace the pod runs in (the script creates it if it is missing),
3. the pod name,
4. one `KEY=PVC` pair per restored volume.

The KEY is the `sourcePVC` of a `restoredVolumes` entry. Give a pair for every entry. A missing
pair is an error. A one-volume check such as `checks/postgres/restorecheck.yaml` needs one pair, `data=r-data`.
List the keys with:

```bash
python3 hack/check-test/render.py checks/nextcloud/restorecheck.yaml --list
```

Environment variables:

| Variable | Default | Meaning |
|---|---|---|
| `TIMEOUT` | `900` | Seconds to wait for the pod to finish. |
| `KEEP` | unset | Set to `1` to keep the pod after the run. |

The output ends with `VERDICT: PASS` or `VERDICT: FAIL`. The script exits 0 on pass.

## Run the full round trip for one app

```bash
hack/check-test/roundtrip.sh nextcloud
```

This runs these steps in namespace `bpt-nextcloud`:

1. Create a Kopiur `Repository` and a `SnapshotPolicy` for each volume the check needs.
2. Apply `checks/nextcloud/seed.yaml`. Wait for the `seed` pod to succeed.
3. For each volume: create a `Snapshot`, wait for `Succeeded`, create a `Restore` and a PVC
   named `r-<volume>`, wait for `Completed` and `Bound`.
4. Run the check against the `r-<volume>` PVCs.
5. Delete the namespace, unless `KEEP=1`.

When every step succeeds, the exit code of `roundtrip.sh` is the exit code of the check. A
failed step before the check exits with 1.

The mover UID for the restore and the source snapshot comes from the `mover-uid` annotation of the
check, keyed by `sourcePVC` (`--uid KEY` prints it). Without a key, it is the first container's
`runAsUser`. That is why every check must set `runAsUser` on its containers.

Environment variables: `TIMEOUT` (per wait, default 900 seconds) and `KEEP`.

Custom paths:

```bash
hack/check-test/roundtrip.sh minimal \
  checks/minimal/seed.yaml \
  checks/minimal/restorecheck.yaml
```

## Round trip through the operator

`roundtrip.sh` skips the operator. `operator-roundtrip.sh` drives it for real with the WordPress
example of one tool, from the repo root:

```bash
hack/check-test/operator-roundtrip.sh kopiur
hack/check-test/operator-roundtrip.sh velero negative
```

Needs the operator running with the provider enabled, the tool installed (Velero with the node
agent), and the S3 endpoint of the kind dev cluster. The namespace is `bret-<tool>` (`NS`), Velero
lives in `VELERO_NS` (default `velero`). `KEEP=1` keeps the namespace.

1. Recreate the namespace, create the S3 Secrets with `kubectl create secret`, and
   `kubectl apply -k examples/<tool>`. Wait for the Deployments and the `seed` Job.
2. Back up both PVCs: Kopiur applies `snapshot.yaml`; VolSync patches a manual trigger into
   both `ReplicationSources`; Velero runs `velero backup create wordpress-1 --snapshot-move-data`.
3. Without `negative`: apply `restore.yaml` and wait for `Succeeded`, then apply
   `restoretest.yaml`, set the run-now annotation, and wait for `lastRun.result: Succeeded`.
4. With `negative`: apply `negative/restore.yaml` (only `html`) and wait for `Failed`. It prints
   the Ready reason, `SourceMissing`.
5. Exit 0 and print `PASS` only when every wait matched.

## Writing a seed

A seed is the only per-app work. It must satisfy this contract:

- One `PersistentVolumeClaim` per `restoredVolumes` entry. The PVC name is the entry's `sourcePVC`
  (`html` and `pgdata` for nextcloud, `data` for one-volume apps).
- One `Pod` named `seed` that writes the data and the canary, then exits 0. The harness
  waits for `Succeeded`.
- A crash-consistent end state is good. The seed may kill the database hard, as the Postgres
  seed does. The check then has to recover, which is part of the test.
- Use the same UID the check uses. If the PVC starts owned by root, add a root init
  container that runs `chown <uid>:<gid>` on the mount, as the seeds do.

Start from `checks/nextcloud/seed.yaml` (two volumes) or `checks/minimal/seed.yaml` (one volume).

## Negative test

A check that can never fail proves nothing. After a pass, break the expected value. For example,
change the canary payload's hash in the seed. Then run the round trip again. The check must
report `VERDICT: FAIL`.

## Reading a failure

`check.sh` prints the exit code of every init and check container. Then it prints the last 40 log lines
of each. Common cases:

| Symptom | Likely cause |
|---|---|
| `init ... exit=` non-zero, `permission denied` in the init log | UID mismatch or a Postgres-style data dir mode. See [checks.md](/guides/write-a-check/#pod-security). |
| Pod stays `Pending` with `persistentvolumeclaim ... not found` | Restored PVC not created or wrong claim name. Check the `KEY=PVC` pairs. |
| `pg_amcheck: no relations to check` | The extension is not installed in the scratch copy. The Postgres check now creates `amcheck` first. |
| `another server might be running; trying to start server anyway` | Harmless. A crash-consistent copy has a stale `postmaster.pid`. |
| Restore never reaches `Completed` | Read the Kopiur `Restore` conditions and the mover pod logs. See [troubleshooting.md](/reference/failure-reasons/). |

## Images

The seed, check and mover pods pull their images from the registry. The scripts do not pull them.
Check that the tag exists before you rely on a check:

```bash
docker manifest inspect postgres:17.12 >/dev/null && echo ok
# or
nerdctl --namespace k8s.io pull postgres:17.12
```

## Status of the app checks

See [checks/README.md](/guides/check-catalog/). Each `checks/<app>/README.md` records whether
the round trip passed, the negative test result, and the open issues.
