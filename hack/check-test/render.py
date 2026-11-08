#!/usr/bin/env python3
"""Render a RestoreCheck into a check Pod, the way the operator is meant to.

Modes:
  render.py CHECK --name POD --claims K=V [K=V ...] [--ns NS]   print YAML (PVCs + Pod)
  render.py CHECK --list                                        print volume keys, one per line
  render.py CHECK --uid KEY                                     print the mover UID for one volume key

Volume keys: restoreMount checks use the key "data".
restoredVolumes checks use the entry name as key.
Scratch PVCs are named <pod>-scratch-<name> and rendered as PVC objects.
"""
import argparse
import json
import re
import sys

import yaml


# Pod-level fields a RestoreCheck may set. Names match the PodSpec fields.
POD_FIELDS = [
    "securityContext",
    "serviceAccountName",
    "imagePullSecrets",
    "nodeSelector",
    "tolerations",
    "affinity",
    "priorityClassName",
    "runtimeClassName",
]


def dns(s):
    return re.sub(r"[^a-z0-9-]", "-", s.lower()).strip("-")[:50]


def volume_keys(spec):
    if spec.get("restoreMount"):
        return ["data"]
    return [v["name"] for v in spec.get("restoredVolumes", [])]


def mount_targets(spec):
    """Return list of (volume_key, mountPath) pairs."""
    if spec.get("restoreMount"):
        return [("data", spec["restoreMount"])]
    return [(v["name"], v["mountPath"]) for v in spec.get("restoredVolumes", [])]


def find_uid(bp, key):
    """UID the mover uses for one volume.

    A check may set the annotation bret.linogics.io/mover-uid to a JSON
    object mapping volume keys to UIDs. Without it, the first main container's runAsUser
    is used. Init containers are skipped, because a native sidecar can run as another UID.
    """
    ann = (bp["metadata"].get("annotations") or {}).get(
        "bret.linogics.io/mover-uid"
    )
    if ann:
        mapping = json.loads(ann)
        if key in mapping:
            return mapping[key]
    for c in bp["spec"]["containers"]:
        uid = (c.get("securityContext") or {}).get("runAsUser")
        if uid is not None:
            return uid
    return None


def render(bp, name, ns, claims, scratch_list):
    spec = bp["spec"]
    init = [dict(c) for c in spec.get("initContainers", [])]
    main = [dict(c) for c in spec["containers"]]
    volumes = []
    docs = []

    for key, mount in mount_targets(spec):
        if key not in claims:
            sys.exit(f"missing --claims {key}=<pvc>; check needs keys {volume_keys(spec)}")
        vname = "restored-" + dns(key)
        volumes.append({"name": vname, "persistentVolumeClaim": {"claimName": claims[key]}})
        for c in init + main:
            c.setdefault("volumeMounts", []).append({"name": vname, "mountPath": mount})

    for sv in scratch_list:
        claim = f"{name}-scratch-{dns(sv['name'])}"
        vname = "scratch-" + dns(sv["name"])
        pvc = {
            "apiVersion": "v1",
            "kind": "PersistentVolumeClaim",
            "metadata": {"name": claim, "namespace": ns},
            "spec": {
                "accessModes": ["ReadWriteOnce"],
                "resources": {"requests": {"storage": str(sv["size"])}},
            },
        }
        if sv.get("storageClassName"):
            pvc["spec"]["storageClassName"] = sv["storageClassName"]
        docs.append(pvc)
        volumes.append({"name": vname, "persistentVolumeClaim": {"claimName": claim}})
        for c in init + main:
            c.setdefault("volumeMounts", []).append({"name": vname, "mountPath": sv["mountPath"]})

    volumes += spec.get("volumes", [])
    pod = {
        "apiVersion": "v1",
        "kind": "Pod",
        "metadata": {
            "name": name,
            "namespace": ns,
            "labels": {"bret.linogics.io/check": dns(bp["metadata"]["name"])},
        },
        "spec": {
            "restartPolicy": "Never",
            "initContainers": init,
            "containers": main,
            "volumes": volumes,
        },
    }
    for field in POD_FIELDS:
        if field in spec:
            pod["spec"][field] = spec[field]
    docs.append(pod)
    return docs


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("check")
    ap.add_argument("--name")
    ap.add_argument("--ns", default="default")
    ap.add_argument("--claims", nargs="*", default=[])
    ap.add_argument("--list", action="store_true")
    ap.add_argument("--uid", metavar="KEY")
    a = ap.parse_args()

    with open(a.check) as f:
        bp = yaml.safe_load(f)
    spec = bp["spec"]

    if a.list:
        print("\n".join(volume_keys(spec)))
        return
    if a.uid is not None:
        uid = find_uid(bp, a.uid)
        print(uid if uid is not None else "")
        return

    claims = dict(kv.split("=", 1) for kv in a.claims)
    name = a.name or "check-" + dns(bp["metadata"]["name"])
    docs = render(bp, name, a.ns, claims, spec.get("scratchVolumes", []))
    print(yaml.safe_dump_all(docs, sort_keys=False, width=200))


if __name__ == "__main__":
    main()
