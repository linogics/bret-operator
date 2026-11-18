---
title: Kopiur
description: How bret restores Kopiur Snapshots with the Kopiur volume populator and checks them.
---

Kopiur backs up PVCs with Kopia. Each `Snapshot` holds one PVC. bret restores each `Snapshot`
into a scratch PVC through a Kopiur `Restore` and runs the check against all of them. Kopiur
support in bret is alpha.

```d2 title="bret restores each Kopiur Snapshot into a scratch PVC through a Kopiur Restore"
direction: down
s3: "S3 bucket\nKopia repository" {shape: cylinder}
ns: "your namespace" {
  snap: "Snapshot\n(one PVC, read by bret)"
  creds: "repository credentials Secret"
  r: "bret Restore"
  kr: "Kopiur Restore\npopulator target"
  pvc: "scratch PVC\ndataSourceRef: Kopiur Restore"
  pod: "check pod"
  r -> snap: "reads PVC, size"
  r -> kr: creates
  r -> pvc: creates
  kr -> creds: uses
  kr -> pvc: populates
  pod -> pvc: mounts
}
s3 -> ns.kr: data
```

## How it works

1. For each source, bret reads the `Snapshot` named by `backupRef` and checks that it backs up
   the `sourcePVC` of the source. Otherwise the `Restore` fails with `SourceMismatch`.
2. It creates a Kopiur `Restore` with `source.snapshotRef`, `target.populator: {}` and
   `policy.onMissingSnapshot: Fail`. When a mover UID is known, it sets
   `mover.securityContext` and `mover.podSecurityContext.fsGroup`.
3. It creates the scratch PVC with a `dataSourceRef` to the Kopiur `Restore`. Kopiur fills it as
   a volume populator.
4. When every Kopiur `Restore` has `status.phase: Completed` and condition `Ready=True`, bret
   runs the check pod with all scratch PVCs mounted.
5. On cleanup the operator deletes the Kopiur `Restore` objects and the scratch PVCs.

Without `size`, bret sizes the scratch PVC from the source with `autoExpand`: by default 10
percent headroom and at least 1Gi. Details:
[Scratch volume size](/concepts/volumes/#scratch-volume-size).

## Next steps

- [Add bret to an existing Kopiur setup](/providers/kopiur/existing-deployment/): the checklist.
- [Walkthrough](/providers/kopiur/walkthrough/): a WordPress + MariaDB example from install to
  `RestoreTest`.
- [Limitations](/providers/kopiur/limitations/): what bret cannot do with Kopiur, and fixes.

## Install Kopiur

Skip this if Kopiur already runs. Install Kopiur with its Helm chart into `kopiur-system`, as the
Kopiur docs describe. Tested with Kopiur 0.11.0 on kind with Kubernetes 1.37. Upstream issue
`kopiur#233` affects restores and is still open.

The provider is on by default in the bret chart (`providers.kopiur.enabled: true`).
