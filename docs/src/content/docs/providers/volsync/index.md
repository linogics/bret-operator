---
title: VolSync
description: How bret restores VolSync backups.
---

VolSync backs up one PVC per `ReplicationSource` with restic. bret treats the last sync of a
`ReplicationSource` as its backup, restores it into a scratch PVC with a `ReplicationDestination`,
and runs the check against all restored PVCs.

Tested with VolSync 0.17.0 and restic on kind with Kubernetes 1.37.

Install:

```bash
helm repo add backube https://backube.github.io/helm-charts/
helm install volsync backube/volsync --namespace volsync-system --create-namespace
```

Enable the provider in the bret chart with `providers.volsync.enabled: true`.
