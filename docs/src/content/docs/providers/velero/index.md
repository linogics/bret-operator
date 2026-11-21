---
title: Velero
description: How bret restores Velero backups into a scratch namespace and checks them.
---

Velero backs up a whole namespace in one `Backup`, with the PVC data moved to S3. bret restores
only the PVCs of that `Backup` into a scratch namespace and runs the check against them. Your app
and its namespace are never touched.

```d2 title="bret restores the PVCs of a Velero Backup into a scratch namespace and runs the check there"
app: my-app-namespace {
  style.font-size: 24
  top: 0
  left: 0
  rc: RestoreCheck {
    top: 0
    left: 0
    width: 180
    height: 80
  }
  r: bret Restore {
    top: 0
    left: 300
    width: 180
    height: 80
  }
}
velero: velero namespace {
  style.font-size: 24
  top: 100
  left: 760
  backup: "Backup\n(read by bret)" {
    top: 0
    left: 0
    width: 220
    height: 100
  }
  restore: "Velero Restore\nPVCs and PVs only" {
    top: 140
    left: 0
    width: 220
    height: 100
  }
}
s3: "S3 bucket\nbackup data" {
  shape: cylinder
  top: 280
  left: 1220
  width: 180
  height: 140
}
scratch: "bret-scratch-<hash>" {
  style.font-size: 24
  top: 520
  left: 0
  pod: check pod {
    top: 0
    left: 0
    width: 180
    height: 100
  }
  pvc: "restored PVCs\n(source names)" {
    top: 0
    left: 300
    width: 180
    height: 100
  }
  pod -> pvc: mounts
}
app.r -> velero.backup: "checks volume\ndata"
app.r -> velero.restore: creates
s3 -> velero.restore: data
velero.restore -> scratch.pvc: restores
app.rc -> scratch.pod: "template,\nSecrets copied"
```

## How it works

1. bret reads the `Backup` in the Velero namespace. It refuses a `Backup` whose volume data a
   PVC-only restore cannot bring back (see [Volume data](/providers/velero/existing-deployment/#1-volume-data-must-be-a-snapshot)).
2. It creates a Velero `Restore` with `includedResources: [persistentvolumeclaims,
   persistentvolumes]`, `includedNamespaces` set to the `Restore` namespace, and a
   `namespaceMapping` to `bret-scratch-<hash>`. No Deployment, Service or Secret of the backup
   is restored.
3. When the Velero `Restore` is `Completed`, bret copies the pod-security labels of your
   namespace and the Secrets and ConfigMaps the check names into the scratch namespace.
4. It runs the check pod there, with each restored PVC mounted under its source name.
5. On cleanup the operator deletes the Velero `Restore` and the scratch namespace.

`<hash>` is the first 12 hex characters of `sha256("<namespace>/<name>")` of the `Restore`.

## Velero labels and annotations bret reads

| Key | On | What bret uses it for |
|---|---|---|
| `velero.io/backup-name` | `PodVolumeBackup` | Finds the file system backups of the `Backup` it is about to restore. |
| `velero.io/pvc-name` | `PodVolumeBackup` (annotation) | Tells whether a source PVC was saved as file system backup. Such a PVC fails with `BackupUnsupported`. |
| `velero.io/schedule-name` | `Backup` | Not read by bret itself. Velero sets it on every `Backup` of a `Schedule`, so it is the usual label for `backupSelector.labels`. |

bret also reads these `Backup` fields: `status.phase`, `status.completionTimestamp`,
`spec.includedNamespaces`, `spec.excludedNamespaces` and `spec.snapshotVolumes`.

## Next steps

- [Add bret to an existing Velero setup](/providers/velero/existing-deployment/): the checklist.
- [Walkthrough](/providers/velero/walkthrough/): a WordPress + MariaDB example from install to
  `RestoreTest`.
- [Limitations](/providers/velero/limitations/): what bret cannot do with Velero, and fixes.

## Install Velero

Skip this if Velero already runs. Tested with Velero 1.18.2 (Helm chart 12.2.5) and the AWS
plugin v1.14.4 on kind with Kubernetes 1.37:

```bash
velero install \
  --provider aws \
  --plugins velero/velero-plugin-for-aws:v1.14.4 \
  --bucket velero \
  --secret-file ./velero-credentials \
  --backup-location-config "region=us-east-1,s3ForcePathStyle=true,s3Url=https://s3.example.internal" \
  --use-node-agent \
  --use-volume-snapshots=false \
  --features=EnableCSI \
  --wait
```

`velero-credentials` is an AWS-style credentials file. Do not commit it:

```ini
[default]
aws_access_key_id = <access key>
aws_secret_access_key = <secret key>
```

The node agent (`--use-node-agent`) moves CSI snapshot data to S3. bret needs that for
`snapshotMoveData`.
