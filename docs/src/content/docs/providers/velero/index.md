---
title: Velero
description: How bret restores Velero backups.
---

Velero backs up a whole namespace in one `Backup`, with the PVC data moved to S3. bret restores
the PVCs of that `Backup` into a scratch namespace and runs the check against them.

Tested with Velero 1.18 (Helm chart 12.2.5) and the AWS plugin on kind with Kubernetes 1.37.

Install:

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

Enable the provider in the bret chart with `providers.velero.enabled: true`. The Velero install
namespace is `providers.velero.namespace`, default `velero`. The chart passes it to the operator
as `VELERO_NAMESPACE`.
