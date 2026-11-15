# Getting started

This tutorial takes you from an empty cluster to a first backup check. It has four parts:

1. Install the operator, and a backup tool with one backup.
2. Run a check by hand, against a restored copy. This works today.
3. Register the check and a schedule with the operator. The operator runs the check on the
   schedule and reports the result.
4. Read the result.

Time: about 30 minutes, excluding image pulls.

## Before you start

You need:

- A Kubernetes cluster, version 1.29 or newer, with a CSI driver that supports volume snapshots.
- `kubectl`, `helm` 3.14 or newer, and `python3` with PyYAML (for the harness).
- One backup tool installed, with at least one backup. Pick one in
  [backup-providers.md](backup-providers.md): Velero, VolSync or Kopiur.
- An application with data to back up. This tutorial uses nextcloud with two PVCs: `html` (files)
  and `pgdata` (database). Replace them with your own app afterwards. An app with one PVC works the
  same way, with one entry everywhere.

Create a namespace for the example. Every object below goes into it:

```bash
kubectl create namespace nextcloud
```

## Step 1: Install the operator

```bash
helm install bret-operator oci://ghcr.io/linogics/charts/bret-operator \
  --version 0.1.0 \
  --namespace bret-operator-system \
  --create-namespace
```

Check the installation:

```bash
kubectl -n bret-operator-system get deploy
kubectl get crd restores.bret.linogics.io restoretests.bret.linogics.io restorechecks.bret.linogics.io
```

The deployment should show `1/1` ready. The three CRDs should be listed.

For a checkout of this repository, run `make helm-sync` first. Then install from
`./charts/bret-operator`. More in [installation.md](installation.md).

## Step 2: Make a backup

Use the setup in [backup-providers.md](backup-providers.md) for your tool. The example below
assumes that you already have the PVCs `html` and `pgdata` in namespace `nextcloud`, and a backup
of each. Both backups must be taken at the same time. The volumes must hold the files that
[examples/restorecheck-multi.yaml](../examples/restorecheck-multi.yaml) reads. Take one backup per PVC,
because one Kopiur `Snapshot` and one VolSync `ReplicationSource` hold one PVC. See the warning in
[backup-providers.md](backup-providers.md#repository-and-policy).

Check that the backup exists:

- Velero: `velero backup get`
- VolSync: `kubectl -n nextcloud get replicationsource`
- Kopiur: `kubectl -n nextcloud get snapshot html-1 pgdata-1` (phase `Succeeded`)

## Step 3: Restore into a scratch volume and run the check by hand

This step does not use the operator. It shows what the operator does for you.

### 3a. Restore the backup into a scratch PVC

With Kopiur, the restore is the `Restore` and `PersistentVolumeClaim` pair from
[backup-providers.md](backup-providers.md#restore). Do it once per PVC. Name the PVCs `r-html` and
`r-pgdata`. You pass these names to the check script in step 3c.

Wait until each `Restore` shows `Completed` and each PVC shows `Bound`:

```bash
kubectl -n restore-test get restore -o jsonpath='{range .items[*]}{.metadata.name} {.status.phase}{"\n"}{end}'
kubectl -n restore-test get pvc r-html r-pgdata
```

### 3b. Write the check

The check for this tutorial is [examples/restorecheck-multi.yaml](../examples/restorecheck-multi.yaml).
It mounts both volumes and tests one file on each. For your application, write a check that reads
your data, as described in [checks.md](checks.md). The full nextcloud check is
[checks/nextcloud.yaml](../checks/nextcloud.yaml). For an app with one PVC, the short form is
[examples/restorecheck-minimal.yaml](../examples/restorecheck-minimal.yaml).

### 3c. Run the check

Run the check against the restored PVC:

```bash
hack/check-test/check.sh examples/restorecheck-multi.yaml restore-test check-nextcloud \
  html=r-html pgdata=r-pgdata
```

The last line should be:

```
VERDICT: PASS (check-nextcloud)
```

If it says `VERDICT: FAIL`, read the logs printed above it. Common causes are in
[testing-checks.md](testing-checks.md#reading-a-failure).

### 3d. Prove the check can fail

Change the check so that it expects the wrong value. Run it again, and confirm `VERDICT: FAIL`. A
check that always passes is not a check. Undo the change afterwards.

## Step 4: Register the check with the operator

Apply the check in the namespace of your restore tests:

```bash
kubectl apply -f examples/restorecheck-multi.yaml
kubectl -n nextcloud get restorechecks
```

The `RestoreCheck` object stores the check. The operator reads it when a test runs. Only
cluster admins should create checks, because a check runs a pod with your chosen image.

## Step 5: Create a restore test with a schedule

Copy [examples/restoretest.yaml](../examples/restoretest.yaml). Then set `provider`, the two
`sources` (one per PVC, each with its backup label selector) and `restoreCheckRef` to your values:

```bash
kubectl apply -f examples/restoretest.yaml
```

The operator now acts on the `RestoreTest`. The object shows the last result in its status:

```bash
kubectl -n nextcloud get restoretests
# NAME                 PROVIDER   LAST        NEXT                   AGE
# nextcloud-nightly    kopiur     Succeeded   2026-11-15T03:17:00Z   2d
```

## Step 6: Run a test on demand

With `triggers.manual: true`, set a new value on the annotation to start a run:

```bash
kubectl -n nextcloud annotate restoretest nextcloud-nightly \
  bret.linogics.io/run-now="$(date +%s)" --overwrite
```

## Step 7: Read the result

- Status: `kubectl -n nextcloud get restoretest nextcloud-nightly -o yaml` shows `status.lastRun`
  with `result`, `time` and `reason`.
- Events: `kubectl -n nextcloud get events --field-selector involvedObject.kind=RestoreTest`
- Metrics: the operator exposes Prometheus metrics on port 8443 (see
  [installation.md](installation.md#metrics-scraping)). Metric names may change before v1.

If a run failed and `keepOnFailure` is `true`, the failed pod, its PVC and the restore object stay
in the namespace. Read the pod logs first. Then delete them.

## Clean up

```bash
kubectl delete restoretest nextcloud-nightly -n nextcloud --ignore-not-found
kubectl delete -f examples/restorecheck-multi.yaml --ignore-not-found
kubectl delete namespace restore-test --ignore-not-found
helm uninstall bret-operator -n bret-operator-system
```

Keep the CRDs if you plan to return. See [installation.md](installation.md#uninstalling) for
how to remove them.

## Next steps

- Write a check for your own app: [checks.md](checks.md).
- Set up schedules, retries and keep-on-failure: [restoretest-guide.md](restoretest-guide.md).
- Apps with a database and files: [data-and-database.md](data-and-database.md).
- Something broke: [troubleshooting.md](troubleshooting.md).
