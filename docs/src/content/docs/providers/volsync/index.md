---
title: VolSync
description: How bret restores the last sync of a VolSync ReplicationSource into a scratch PVC and checks it.
---

VolSync backs up one PVC per `ReplicationSource` with restic. VolSync has no object per backup:
bret treats the last sync of a `ReplicationSource` as its backup. It restores that sync into a
scratch PVC with a `ReplicationDestination` and runs the check against all restored PVCs.

```d2 title="bret restores each ReplicationSource into a scratch PVC with a ReplicationDestination"
direction: down
s3: "S3 bucket\none restic repository per PVC" {shape: cylinder}
ns: "your namespace" {
  rs: "ReplicationSource\n(one per PVC, read by bret)"
  secret: "restic repository Secret"
  r: "bret Restore"
  rd: "ReplicationDestination\nmanual trigger, restoreAsOf"
  pvc: "scratch PVC\n<restore>-<sourcePVC>-<hash>"
  pod: "check pod"
  r -> rs: "reads repository,\nsourcePVC, lastSyncTime"
  r -> rd: creates
  rd -> secret: uses
  rd -> pvc: "restores into"
  pod -> pvc: mounts
}
s3 -> ns.rd: data
```

## How it works

1. For each source, bret reads the `ReplicationSource` named by `backupRef`. It takes the
   repository Secret name from `spec.restic.repository` and checks that `spec.sourcePVC`
   matches the source.
2. It creates a plain scratch PVC and a `ReplicationDestination` with:
   - `trigger.manual` set to the `Restore` name, so it runs once,
   - `restic.copyMethod: Direct` and `destinationPVC` set to the scratch PVC,
   - `restic.restoreAsOf`, when the source has one, to pin the restic snapshot,
   - `restic.moverSecurityContext` with the mover UID (see
     [Mover UID](/providers/volsync/existing-deployment/#3-set-the-mover-uid)).
3. When every `ReplicationDestination` reports `status.latestImage.kind:
   PersistentVolumeClaim`, it runs the check pod with all scratch PVCs mounted.
4. On cleanup the operator deletes the `ReplicationDestination` objects and the scratch PVCs.

A `RestoreTest` pins every run: each `Restore` source gets `restoreAsOf` set to the
`lastSyncTime` it picked. A sync that lands during the run does not change what is tested.

## Next steps

- [Add bret to an existing VolSync setup](/providers/volsync/existing-deployment/): the checklist.
- [Walkthrough](/providers/volsync/walkthrough/): a WordPress + MariaDB example from install to
  `RestoreTest`.
- [Limitations](/providers/volsync/limitations/): what bret cannot do with VolSync, and fixes.

## Install VolSync

Skip this if VolSync already runs. Tested with VolSync 0.17.0 and restic on kind with
Kubernetes 1.37:

```bash
helm repo add backube https://backube.github.io/helm-charts/
helm install volsync backube/volsync --namespace volsync-system --create-namespace
```
