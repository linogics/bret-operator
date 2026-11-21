---
title: VolSync
description: How bret restores the last sync of a VolSync ReplicationSource into a scratch PVC and checks it.
---

VolSync backs up one PVC per `ReplicationSource` with restic. VolSync has no object per backup:
bret treats the last sync of a `ReplicationSource` as its backup. It restores that sync into a
scratch PVC with a `ReplicationDestination` and runs the check against all restored PVCs.

```d2 title="bret restores each ReplicationSource into a scratch PVC with a ReplicationDestination"
ns: my-app-namespace {
  style.font-size: 24
  top: 0
  left: 0
  r: bret Restore {
    top: 0
    left: 640
    width: 280
    height: 100
  }
  rs: "ReplicationSource\n(one per PVC,\nread by bret)" {
    top: 0
    left: 1120
    width: 220
    height: 100
  }
  pod: check pod {
    top: 160
    left: 0
    width: 140
    height: 100
  }
  pvc: "scratch PVC\n<restore>-\n<sourcePVC>-<hash>" {
    top: 160
    left: 260
    width: 220
    height: 100
  }
  rd: "ReplicationDestination\nmanual trigger,\nrestoreAsOf" {
    top: 160
    left: 640
    width: 280
    height: 100
  }
  secret: "restic repository\nSecret" {
    top: 160
    left: 1120
    width: 220
    height: 100
  }
  r -> rs: "reads repository,\nsourcePVC,\nlastSyncTime"
  r -> rd: creates
  rd -> secret: uses
  rd -> pvc: "restores\ninto"
  pod -> pvc: mounts
}
s3: "S3 bucket\none restic repository\nper PVC" {
  shape: cylinder
  top: 440
  left: 700
  width: 280
  height: 160
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
