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
- An application with data to back up. This tutorial uses a small example volume, so you can
  follow it end to end. Replace it with your own app afterwards.

Create a namespace for the example. Every object below goes into it:

```bash
kubectl create namespace data
```

## Step 1: Install the operator

```bash
helm install bret-operator oci://ghcr.io/linogics/charts/bret-operator \
  --version 0.1.0 \
  --namespace bret-operator-system \
  --create-namespace
```

Check it:

```bash
kubectl -n bret-operator-system get deploy
kubectl get crd restores.bret.linogics.io restoretests.bret.linogics.io restorechecks.bret.linogics.io
```

The deployment should show `1/1` ready. The three CRDs should be listed.

For a checkout of this repository, run `make helm-sync` first, then install from
`./charts/bret-operator`. More in [installation.md](installation.md).

## Step 2: Make a backup

Use the setup in [backup-providers.md](backup-providers.md) for your tool. The example below
assumes you already have a PVC named `orders-db-data` in namespace `data` and a backup of it
under the name `orders-db-1`.

Check that the backup exists:

- Velero: `velero backup get`
- VolSync: `kubectl -n data get replicationsource`
- Kopiur: `kubectl -n data get snapshot orders-db-1` (phase `Succeeded`)

## Step 3: Restore into a scratch volume and run the check by hand

This step does not use the operator. It shows what the operator will do for you.

### 3a. Restore the backup into a scratch PVC

With Kopiur, the restore is the `Restore` and `PersistentVolumeClaim` pair from
[backup-providers.md](backup-providers.md#restore). Name the PVC `r-data`. The check expects
that name.

Wait until the `Restore` shows `Completed` and the PVC shows `Bound`:

```bash
kubectl -n restore-test get restore orders-db-restore -o jsonpath='{.status.phase}{"\n"}'
kubectl -n restore-test get pvc orders-db-restored
```

### 3b. Write the check

The smallest check is in [examples/restorecheck-minimal.yaml](../examples/restorecheck-minimal.yaml).
It reads one file and checks its hash. For your application, write a check that reads your
data, as described in [checks.md](checks.md).

### 3c. Run the check

Run the check against the restored PVC:

```bash
hack/check-test/check.sh examples/restorecheck-minimal.yaml restore-test check-minimal data=orders-db-restored
```

The last line should be:

```
VERDICT: PASS (check-minimal)
```

If it says `VERDICT: FAIL`, read the logs printed above it. Common causes are in
[testing-checks.md](testing-checks.md#reading-a-failure).

### 3d. Prove the check can fail

Change the check so it expects the wrong value, run it again, and confirm `VERDICT: FAIL`. A
check that always passes is not a check. Undo the change afterwards.

## Step 4: Register the check with the operator

Apply the check in the namespace of your restore tests:

```bash
kubectl apply -f examples/restorecheck-minimal.yaml
kubectl -n data get restorechecks
```

The `RestoreCheck` object stores the check. The operator reads it when a test runs. Only
cluster admins should create checks, because a check runs a pod with your chosen image.

## Step 5: Create a restore test with a schedule

Copy [examples/restoretest.yaml](../examples/restoretest.yaml), then set `provider`, the backup
label selector and `restoreCheckRef` to your values:

```bash
kubectl apply -f examples/restoretest.yaml
```

The operator now acts on the `RestoreTest`. The object shows the last result in its status:

```bash
kubectl -n data get restoretests
# NAME                 PROVIDER   LAST        NEXT                   AGE
# orders-db-nightly    volsync    Succeeded   2026-11-15T03:17:00Z   2d
```

Provider `volsync` needs the VolSync provider enabled in the chart. See
[backup-providers.md](backup-providers.md).

## Step 6: Run a test on demand

With `triggers.manual: true`, set a new value on the annotation to start a run:

```bash
kubectl -n data annotate restoretest orders-db-nightly \
  bret.linogics.io/run-now="$(date +%s)" --overwrite
```

## Step 7: Read the result

- Status: `kubectl -n data get restoretest orders-db-nightly -o yaml` shows `status.lastRun`
  with `result`, `time` and `reason`.
- Events: `kubectl -n data get events --field-selector involvedObject.kind=RestoreTest`
- Metrics: the operator exposes Prometheus metrics on port 8443 (see
  [installation.md](installation.md#metrics-scraping)). Metric names are not final.

If a run failed and `keepOnFailure` is `true`, the failed pod, its PVC and the restore object stay
in the namespace. Read the pod logs first, then delete them.

## Clean up

```bash
kubectl delete restoretest orders-db-nightly -n data --ignore-not-found
kubectl delete -f examples/restorecheck-minimal.yaml --ignore-not-found
kubectl delete namespace restore-test --ignore-not-found
helm uninstall bret-operator -n bret-operator-system
```

Keep the CRDs if you plan to come back. See [installation.md](installation.md#uninstalling) for
removing them.

## Next steps

- Write a check for your own app: [checks.md](checks.md).
- Set up schedules, retries and keep-on-failure: [restoretest-guide.md](restoretest-guide.md).
- Apps with a database and files: [data-and-database.md](data-and-database.md).
- Something broke: [troubleshooting.md](troubleshooting.md).
