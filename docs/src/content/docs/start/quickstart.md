---
title: Quickstart
description: From an empty kind cluster to a first passing restore test with Kopiur.
---

This tutorial takes you from an empty cluster to a restore test that runs on a schedule. It uses
WordPress + MariaDB with two PVCs: `html` (the docroot) and `mysql` (the database). It works the
same with Kopiur, VolSync or Velero. Only the backup step differs; it is on the page of your tool.

1. Install the operator.
2. Deploy the example app and back it up, as your tool's page shows.
3. Run one `Restore` by hand, and prove that the check can fail.
4. Create a `RestoreTest` with a schedule, and run it on demand.
5. Read the result.

Time: about 30 minutes, excluding image pulls.

## Before you start

You need:

- A Kubernetes cluster, version 1.29 or newer, with a CSI driver that supports volume snapshots.
- `kubectl` and `helm` 3.14 or newer.
- One backup tool installed, and an S3 bucket for it. Pick one in
  [providers/](/providers/): Kopiur, VolSync or Velero.
- A checkout of this repository, for the files in [examples/](https://github.com/linogics/bret-operator/tree/main/examples).

## Step 1: Install the operator

```bash
helm install bret-operator oci://ghcr.io/linogics/charts/bret-operator \
  --version 0.1.0 \
  --namespace bret-operator-system \
  --create-namespace \
  --set providers.volsync.enabled=true \
  --set providers.velero.enabled=true
```

Kopiur is on by default. Enable only the tools you run. Check the installation:

```bash
kubectl -n bret-operator-system get deploy
kubectl get crd restores.bret.linogics.io restoretests.bret.linogics.io restorechecks.bret.linogics.io
```

The deployment should show `1/1` ready. The three CRDs should be listed. More in
[installation.md](/start/installation/).

## Step 2: Deploy the app and back it up

Follow steps 1 and 2 on the page of your tool. They deploy the app into the namespace
`wordpress`, seed a canary, and take one backup of both PVCs:

- [providers/kopiur.md](/providers/kopiur/)
- [providers/volsync.md](/providers/volsync/)
- [providers/velero.md](/providers/velero/)

The example also applies the check,
[checks/wordpress/restorecheck.yaml](https://github.com/linogics/bret-operator/blob/main/checks/wordpress/restorecheck.yaml). It mounts both
restored volumes, starts MariaDB next to WordPress, and checks that the canary post matches the
canary file. For your own app, write a check as described in [checks.md](/guides/write-a-check/). Only
cluster admins should create checks, because a check runs a pod with your chosen image.

## Step 3: Restore by hand

Replace `<tool>` with `kopiur`, `volsync` or `velero`:

```bash
kubectl apply -f examples/<tool>/restore.yaml -n wordpress
kubectl -n wordpress wait --for=jsonpath='{.status.phase}'=Succeeded restore/wordpress-manual --timeout=15m
```

The operator restores both PVCs into scratch volumes, runs the check once against both, and sets
the phase. Then prove that the check can fail: the negative `Restore` restores only `html`.

```bash
kubectl apply -f examples/<tool>/negative/restore.yaml -n wordpress
kubectl -n wordpress wait --for=jsonpath='{.status.phase}'=Failed restore/wordpress-negative --timeout=15m
```

A check that always passes is not a check. To run a check without the operator, against PVCs you
restored yourself, see [testing-checks.md](/guides/test-a-check/).

## Step 4: Create a restore test

```bash
kubectl apply -f examples/<tool>/restoretest.yaml -n wordpress
kubectl -n wordpress get restoretests
# NAME        PROVIDER   LAST        NEXT                   AGE
# wordpress   kopiur     Succeeded   2026-11-17T03:17:00Z   2d
```

The test runs every night, after each new backup, and on demand. Each run picks the newest backup
of every PVC. How each tool's backups are picked: "How bret picks backups" on the tool page.

With `triggers.manual: true`, set a new value on the annotation to start a run now:

```bash
kubectl -n wordpress annotate restoretest wordpress \
  bret.linogics.io/run-now="$(date +%s)" --overwrite
```

## Step 5: Read the result

- Status: `kubectl -n wordpress get restoretest wordpress -o yaml` shows `status.lastRun` with
  `result`, `time` and `reason`.
- Events: `kubectl -n wordpress get events --field-selector involvedObject.kind=RestoreTest`
- Metrics: the operator exposes Prometheus metrics on port 8443 (see
  [installation.md](/start/installation/#metrics-scraping)). Metric names may change before v1.

If a run failed and `keepOnFailure` is `true`, the failed pod, its PVCs and the restore objects
stay. Read the pod logs first. Then delete them.

## Clean up

```bash
kubectl delete namespace wordpress
helm uninstall bret-operator -n bret-operator-system
```

Delete the backups with your tool. Keep the CRDs if you plan to return. See
[installation.md](/start/installation/#uninstalling) for how to remove them.

## Next steps

- Write a check for your own app: [checks.md](/guides/write-a-check/).
- Set up schedules, retries and keep-on-failure: [restoretest-guide.md](/guides/restore-tests/).
- Apps with a database and files: [data-and-database.md](/guides/data-and-database/).
- Something broke: [troubleshooting.md](/reference/failure-reasons/).
