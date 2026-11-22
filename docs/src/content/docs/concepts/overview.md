---
title: How bret works
description: The problem bret solves, its three objects and what it does and does not do.
---

This page explains what bret does and the rules it follows. The schema is in
[API reference](/reference/api/).

## The problem

A backup that you never restored is not proven. bret restores your backups
on a schedule, checks the restored data, and tells you when the check fails.

## The three objects

| Kind | Short name | Purpose | Who writes it |
|---|---|---|---|
| `RestoreCheck` | `rcheck` | The check: a pod template that runs against restored data. Exit code 0 means pass. | You, once per app. |
| `RestoreTest` | `rtest` | The schedule and policy: which backups, which check, when, how many retries. | You. |
| `Restore` | `restore` | One restore attempt run: the native restore object plus the check pod. You create it by hand, or a `RestoreTest` creates it. | You, or the operator. |

A typical setup has one `RestoreCheck` per application. It also has one `RestoreTest` for that
app, with one source per PVC.

The diagram shows how the objects refer to each other. Solid arrows create an object, dashed
arrows only reference it. Objects in the namespace of the `Restore` get it as their owner, so deleting
the `Restore` deletes them. Objects in other namespaces (the Velero scratch namespace) are found by
name and label and removed by the `bret.linogics.io/cleanup` finalizer.

```d2 title="How RestoreTest, Restore, RestoreCheck and the objects bret creates refer to each other"
rt: RestoreTest {
  top: 0
  left: 280
  width: 220
  height: 80
}
rc: RestoreCheck {
  top: 0
  left: 660
  width: 160
  height: 80
}
r: "Restore\n(one per attempt)" {
  top: 140
  left: 280
  width: 220
  height: 100
}
yours: my-restore-namespace {
  style.font-size: 24
  top: 300
  left: 0
  native: "VolSync or Kopiur\nrestore object" {
    top: 0
    left: 0
    width: 220
    height: 100
  }
  pvc: scratch PVC {
    top: 140
    left: 0
    width: 220
    height: 80
  }
  pod: check pod {
    top: 280
    left: 0
    width: 220
    height: 80
  }
  pod -> pvc: mounts
}
velero: Velero only {
  style.font-size: 24
  top: 300
  left: 420
  vr: "Velero Restore\nin the velero\nnamespace" {
    top: 0
    left: 0
    width: 280
    height: 100
  }
  scratch: "bret-scratch-<hash>" {
    style.font-size: 24
    top: 140
    left: 0
    pvc: restored PVC {
      top: 0
      left: 0
      width: 160
      height: 80
    }
    pod: check pod {
      top: 140
      left: 0
      width: 160
      height: 80
    }
    pod -> pvc: mounts
  }
}
rt -> r: creates
rt -- rc: {
  style.stroke-dash: 4
  source-arrowhead.label: references
}
r -- rc: {
  style.stroke-dash: 4
  source-arrowhead.label: references
}
r -> yours: creates
r -> velero: creates
```

## What the operator does, and what it does not do

It does:

- create the native restore object of the chosen tool (Velero `Restore`, VolSync
  `ReplicationDestination`, or Kopiur `Restore`), pointed at a scratch volume,
- wait for that restore to finish,
- create the check pod from the check, with the restored volume mounted,
- take the check pod's exit code as the verdict,
- write the result to `status`, to Events, and to Prometheus metrics,
- clean up pods and volumes, or keep them for debugging when you ask it to.

It does not:

- copy, deduplicate or store backup data,
- install Velero, VolSync or Kopiur, or configure their storage,
- read the check's logs or parse its output. Only the exit code counts.

## Providers

| Provider | Backup the operator reads | Native object created | Restore done when |
|---|---|---|---|
| `kopiur` | `Snapshot`, phase `Succeeded`, one PVC each | `kopiur.home-operations.com/v1alpha1` `Restore` | `status.phase` is `Completed` and condition `Ready` is `True` |
| `volsync` | `ReplicationSource`, its `status.lastSyncTime`, one PVC each | `volsync.backube/v1alpha1` `ReplicationDestination` with a manual trigger, pinned with `restoreAsOf` | `status.latestImage.kind` is `PersistentVolumeClaim` |
| `velero` | `Backup` in the Velero namespace, phase `Completed`, every PVC of the namespace | `velero.io/v1` `Restore` into a scratch namespace | `status.phase` is `Completed`, `PartiallyFailed`, `Failed` or `FailedValidation` |

Every source names its `sourcePVC`, for all three. Setup, the namespaces each tool uses, and how
the backups are picked are on the tool pages: [Kopiur](/providers/kopiur/),
[VolSync](/providers/volsync/), [Velero](/providers/velero/).

Enable providers in the Helm chart with `providers.<name>.enabled`. Kopiur is on by
default. Velero and VolSync are off by default. A `Restore` for a disabled provider ends `Failed`
with reason `ProviderUnsupported`. A `RestoreTest` for it waits with that reason.


## Scope

Namespaces:

- The operator watches all namespaces by default.
- `watchNamespaces` in the chart values is a list. Empty means cluster-wide. The table shows what
  it limits.

| Mode | Controller cache | Pod and PVC rights | Velero rights |
|---|---|---|---|
| Cluster-wide (`watchNamespaces` empty) | All namespaces. | `ClusterRole`. | `ClusterRole`, when Velero is on. |
| `watchNamespaces` set | Only those namespaces. | One `Role` and `RoleBinding` per listed namespace. | None. Velero is off. |
| `watchNamespaces` set, Velero on | `Restore`, `RestoreTest` and `RestoreCheck`: only those namespaces. Pods and PVCs: all namespaces. | `ClusterRole`, because the scratch namespaces are not watched. | `Role` in `providers.velero.namespace`, even if it is not listed. |

- A `RestoreTest` in a namespace that is not watched is ignored. The operator writes nothing to it.
- Pods and volumes live in the namespace of the `Restore` or `RestoreTest`. For Velero, they live
  in the scratch namespace.
- With Velero on, the operator can read and create Secrets and ConfigMaps in every namespace. It
  copies the ones a check names into the scratch namespace, which is not watched.
- Velero backups live in the Velero namespace (`velero` by default). The operator reads
  backup objects there. In namespaced mode, this is the one cross-namespace read it needs.
