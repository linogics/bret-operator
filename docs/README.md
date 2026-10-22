# bret-operator docs

bret-operator is a Kubernetes operator that tests your backups. It starts a restore
from Velero, VolSync or Kopiur into a scratch volume, runs your own check in a
throwaway pod, and records the result as CR status, Kubernetes Events and Prometheus
metrics. The operator does not move backup data itself. It drives each tool's own
restore object, then checks the result.

Status: design stage. The Go API types in `api/v1alpha1/` are scaffolds with a
placeholder `Foo` field. The controller does not exist yet. These docs describe the
design. Anything not yet in the code is marked **planned**.

The blueprints in `blueprints/` are untested. None were run.

## Files

| File | What it covers |
|---|---|
| [concepts.md](concepts.md) | What the operator does, providers, scratch-PVC rule, restore test lifecycle, triggers, scope |
| [crds.md](crds.md) | Reference for `Restore`, `RestoreTest`, `RestoreBlueprint`: fields, status, conditions |
| [blueprints.md](blueprints.md) | How to write a `RestoreBlueprint` check, canary convention, using `blueprints/` |
| [best-practices.md](best-practices.md) | Rules for backups and restore tests |
| [data-and-database.md](data-and-database.md) | Apps with files and a database: keeping them in one backup point |
| [spike-findings.md](spike-findings.md) | Verified facts and pitfalls from the kind spike, with tool versions |

Design source: `superpowers/specs/2026-11-14-bret-operator-restore-test-design.md`.
