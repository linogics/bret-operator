---
title: Kopiur
description: How bret restores Kopiur backups.
---

Kopiur backs up one PVC per `Snapshot` with Kopia. bret restores each `Snapshot` into a scratch
PVC and runs the check against all of them.

Tested with Kopiur 0.11.0 on kind with Kubernetes 1.37. Upstream issue
`kopiur#233` affects restores and is still open.

Install Kopiur with its Helm chart into `kopiur-system`, as the Kopiur docs describe.

Enable the provider in the bret chart with `providers.kopiur.enabled`. It is on by default.
