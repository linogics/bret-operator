# Plan: controllers for Restore, RestoreTest and RestoreBlueprint

Date: 2026-11-15. Source of truth for behavior: `docs/superpowers/specs/2026-11-14-bret-operator-restore-test-design.md`.
This plan records decisions taken for the first implementation. Where it differs from the design, this plan wins.

## Scope of this round

1. `Restore` reconciler: one attempt, Kopiur provider only.
2. `RestoreTest` reconciler: triggers, overlap policy, retries (creates one `Restore` per attempt).
3. Status, Kubernetes Events and Prometheus metrics for both kinds.
4. Tests for each step: envtest or controller-runtime fake client, and unit tests for pure logic.

Not in this round: Velero and VolSync providers (return condition `ProviderUnsupported`), namespaced
`watchNamespaces` mode, `RestoreBlueprint` validation beyond the CRD.

## Decisions

- **Provider objects are unstructured.** Create and read Kopiur objects with `unstructured.Unstructured`
  and GVK strings. Do not add the Kopiur Go module. No new API types for providers.
- **Kopiur attempt flow** (spike-verified, see `docs/backup-providers.md`):
  1. Scratch PVC `<restore>-scratch` in the Restore's namespace. `spec.target.storageClassName`,
     `spec.target.size`. `dataSourceRef` points to the Kopiur `Restore` object `<restore>-kopiur`.
  2. Kopiur `Restore` object `<restore>-kopiur`: `spec.source.snapshotRef` = `spec.source.backupRef`
     (name and namespace), `spec.target.populator: {}`, `spec.policy.onMissingSnapshot: Fail`.
     Set `spec.mover.securityContext.runAsUser/runAsGroup` to the UID of the blueprint's
     `restore.bret.linogics.io/mover-uid` annotation for the volume (first main container `runAsUser` when absent),
     and `spec.mover.podSecurityContext.fsGroup` to the same UID.
  3. Wait until the Kopiur `Restore` `status.phase` is `Completed` (done) or `Failed` (attempt fails).
  4. Check pod `<restore>-check` built from the referenced `RestoreBlueprint`, same rules as
     `hack/blueprint-test/render.py`: restored volume mounted at `restoreMount` (or the entry's `mountPath`),
     scratch volumes from `scratchVolumes`, pod-level fields from `POD_FIELDS`, `restartPolicy: Never`.
     Only `restoreMount` blueprints are supported in this round. `restoredVolumes` returns `BlueprintUnsupported`.
  5. Wait for the pod: `Succeeded` (pass) or `Failed` (fail). Pod timeout comes from the restore window.
- **Owner references.** Every object the Restore creates has an owner reference to the Restore, so
  deleting the Restore garbage-collects them.
- **Cleanup.** On `Succeeded`, delete the check pod, the Kopiur `Restore` and the scratch PVC. On `Failed`
  or `Canceled`, delete them unless `spec.keepOnFailure` is true. Set label
  `restore.bret.linogics.io/retained: "true"` when kept.
- **Window.** `status.deadline` = `status.startTime` + `spec.restoreWindow`. Past the deadline, phase
  `Canceled`, cleanup as above.
- **Requeue, no provider watches.** Requeue after 10 seconds while work is in progress. Do not watch
  Kopiur objects in this round.
- **Retries live in RestoreTest.** A `Restore` is one attempt and is terminal once it is `Succeeded`,
  `Failed` or `Canceled`. `RestoreTest` creates the next `Restore` after `retries.backoff`.
- **Naming.** A `RestoreTest` run creates `Restore` objects named `<test>-<shortid>-a<attempt>`, where
  `shortid` is 8 hex characters of a hash of the trigger source (cron tick time, backup name, or run-now nonce).
  Label each `Restore` with `restore.bret.linogics.io/test: <test>` and `restore.bret.linogics.io/attempt: "<n>"`.
- **Triggers.** Cron: the `robfig/cron/v3` parser (add the dependency). afterBackup: list Kopiur `Snapshot`
  objects in the RestoreTest's namespace whose `status.phase` is `Succeeded`, match `backupSelector`
  (name or labels), take each name not yet in `status.handledBackups`, run at `completion time + delay`.
  Manual: `restore.bret.linogics.io/run-now` annotation; a new value starts one run.
- **Overlap.** `Skip` drops the trigger with an Event. `Queue` keeps one pending run. `Replace` deletes the
  running `Restore` (it is canceled) and starts a new one.
- **Status.** Use the existing fields. Conditions: `Ready` on each kind. Event reasons: `RestoreStarted`,
  `RestoreSucceeded`, `RestoreFailed`, `RestoreCanceled`, `CheckPassed`, `CheckFailed`, `RunSkipped`,
  `RunReplaced`, `ProviderUnsupported`, `BlueprintNotFound`, `BlueprintUnsupported`.
- **Metrics** (controller-runtime registry): `bret_restore_runs_total{provider,result}` counter,
  `bret_restore_duration_seconds{provider,result}` histogram, `bret_restoretest_last_success_timestamp_seconds{restoretest,namespace}` gauge.
- **RBAC markers** for everything the reconcilers touch: PVCs, Pods, Events, RestoreBlueprints (get, list, watch),
  Kopiur `restores`, `snapshots`, and the RestoreTest and Restore status subresources.

## Rules for implementers

- Go style: wsl_v5 is enabled. Keep one blank line between logical steps, a blank line before `if`/`for`/`return` blocks,
  and an `if err != nil` directly under the call that sets `err`.
- No Claude or AI attribution in code, comments or commits. Comments in English.
- Run `make manifests generate` after changing types. Run `go build ./...` and `go test ./...` before reporting.
- Do not edit `docs/superpowers/`, `blueprints/`, `hack/`, `charts/`, `examples/`. Report what should change there instead.
- Do not commit. The coordinator commits.
