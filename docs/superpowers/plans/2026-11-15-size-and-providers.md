# Size rule, providers and API group Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Derive the scratch volume size per provider (with `autoExpand`), add Velero and VolSync providers, gate RBAC and controllers per provider in the chart, add `watchNamespaces`, and rename the API group to `bret.linogics.io`.

**Architecture:** Each provider adapter returns a source size and a "known" flag. A shared size function applies `target.size` or `autoExpand`, and fails with `SizeUnknown` or `InsufficientCapacity`. Chart flags control which controllers start and which RBAC rules render.

**Tech Stack:** Go, Operator SDK v1.42.3, controller-runtime v0.21, envtest, Helm, golangci-lint v2.15.0.

**Spec:** `docs/superpowers/specs/2026-11-15-size-and-providers-design.md`

## Global Constraints

- API group: `bret.linogics.io`, version `v1alpha1`. Kinds `Restore`, `RestoreTest`, `RestoreBlueprint`.
- Annotation and label prefix: `bret.linogics.io/` (retained label, mover UID annotation).
- Default `autoExpand`: `percentage: 10`, `min: 1Gi`, no `max`.
- Conditions: `SizeUnknown`, `InsufficientCapacity`.
- Commits: conventional, short, no attribution lines.
- Lint: `golangci-lint run` must report 0 issues (wsl_v5, goconst, unparam, staticcheck).

## Review Focus

1. `target.size` set smaller than the source size: must fail with `InsufficientCapacity`, not create a PVC.
2. VolSync source PVC deleted before the Restore starts, `target.size` omitted: must fail with `SizeUnknown`.
3. `autoExpand` with `min` greater than `max`: must be rejected by CEL on apply.
4. `autoExpand` with `percentage: 0` or both `size` and `percentage` missing: must be rejected by CEL.
5. Two backups in one RestoreTest run with the same timestamp: the watermark must not skip the second one.

Each line gets its test in the owning task (Tasks 2, 3, 6, 7).

---

### Task 1: API group rename

**Files:**
- Modify: `api/v1alpha1/groupversion_info.go`
- Modify: `api/v1alpha1/*.go` (kubebuilder markers, retained label and annotation constants)
- Modify: `internal/controller/restore_objects.go` (`retainedLabel`)
- Modify: `internal/controller/restore_kopiur.go` (`moverUIDAnnotation`)
- Modify: `config/`, `charts/bret-operator/crds/`, `charts/bret-operator/templates/` (regenerated)
- Test: `internal/controller/*_test.go` (existing tests must pass unchanged in logic)

**Interfaces:**
- Consumes: none.
- Produces: group `bret.linogics.io`. Other tasks reference `restorev1alpha1.GroupVersion` only.

- [ ] **Step 1: Change the group in `groupversion_info.go`**

Set `Group: "bret.linogics.io"`.

- [ ] **Step 2: Update constants**

`retainedLabel = "bret.linogics.io/retained"`, mover UID annotation `bret.linogics.io/mover-uid`.

- [ ] **Step 3: Regenerate**

Run: `make manifests generate`
Expected: CRDs under `config/crd/bases/` show group `bret.linogics.io`.

- [ ] **Step 4: Run unit tests**

Run: `make test`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "refactor: rename API group to bret.linogics.io"
```

### Task 2: Size rule and autoExpand

**Files:**
- Create: `internal/controller/size.go`
- Create: `internal/controller/size_test.go`
- Modify: `api/v1alpha1/restore_types.go` (`Target.Size` optional, add `AutoExpand *AutoExpand`)
- Modify: `api/v1alpha1/restore_types.go` (condition reasons `SizeUnknown`, `InsufficientCapacity`)

**Interfaces:**
- Consumes: none.
- Produces:
  - `type AutoExpand struct { Percentage int32; Min *resource.Quantity; Max *resource.Quantity }` with CEL rules: `percentage > 0`, `min <= max` when both set.
  - `func resolveSize(target RestoreTarget, source *resource.Quantity) (resource.Quantity, error)`. Returns `ErrSizeUnknown` when `source == nil` and `target.Size` is unset. Returns `ErrInsufficientCapacity` when `target.Size` is set and `source` is set and `target.Size < source`. Otherwise: if `target.Size` set, return it. If `autoExpand` nil, use the default. Compute `clamp(source*percentage/100, min, max)`.

- [ ] **Step 1: Write failing tests**

In `size_test.go`, table test for `resolveSize` covering:
- target.size set, source nil → target.size.
- target.size nil, source nil → `ErrSizeUnknown`.
- target.size 1Gi, source 2Gi → `ErrInsufficientCapacity`.
- source 10Mi, default → 1Gi (min applies).
- source 10Gi, percentage 10, min 1Gi → 1Gi (10Gi × 10% = 1Gi, exact value).
- source 100Gi, percentage 10, max 5Gi → 5Gi.
- source 3Gi, percentage 100, min 5Gi, max 5Gi → 5Gi.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/controller/ -run TestResolveSize -v`
Expected: FAIL, `resolveSize` undefined.

- [ ] **Step 3: Implement `resolveSize` in `size.go`**

Use `resource.Quantity` integer math on bytes. Errors defined as package vars.

- [ ] **Step 4: Add CEL rules**

On `AutoExpand`: `self.percentage > 0`; on the struct: `!has(self.min) || !has(self.max) || self.min <= self.max`. Regenerate with `make manifests`.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/controller/ -run TestResolveSize -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "feat: derive restore size with autoExpand"
```

### Task 3: Provider size adapters

**Files:**
- Create: `internal/controller/provider_size.go`
- Create: `internal/controller/provider_size_test.go`
- Modify: `internal/controller/restore_kopiur.go` (read `status.stats.sizeBytes` from the Snapshot)

**Interfaces:**
- Consumes: `resolveSize`, `ErrSizeUnknown` from Task 2.
- Produces: `func (r *RestoreReconciler) sourceSize(ctx context.Context, restore *restorev1alpha1.Restore) (*resource.Quantity, error)`. Dispatches on `restore.Spec.Provider`:
  - Kopiur: Snapshot `status.stats.sizeBytes` as a Quantity.
  - Velero: PVC request from backup contents. Read via a `velero.io/v1` DownloadRequest of type `BackupContents`. If unreadable, return nil (not an error).
  - VolSync: source PVC named in ReplicationSource `spec.sourcePVC`; its `spec.resources.requests.storage`. If the PVC is missing, return nil.

- [ ] **Step 1: Write failing tests with fake client**

Kopiur: a Snapshot with `status.stats.sizeBytes: 10485760` → 10Mi. VolSync: ReplicationSource and PVC present → PVC request. VolSync: PVC missing → nil.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/controller/ -run TestSourceSize -v`
Expected: FAIL, `sourceSize` undefined.

- [ ] **Step 3: Implement the three branches**

Velero branch is a stub returning nil, with a code comment `shortcut: Velero contents read not implemented, requires target.size, upgrade in Task 5`. Replace the stub in Task 5.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/controller/ -run TestSourceSize -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: read source size per provider"
```

### Task 4: Wire size into the Restore controller

**Files:**
- Modify: `internal/controller/restore_controller.go` (call `sourceSize` and `resolveSize` before creating the PVC; set conditions)
- Modify: `internal/controller/restore_controller_test.go` (add cases)

**Interfaces:**
- Consumes: `sourceSize` (Task 3), `resolveSize` (Task 2).
- Produces: Restore status condition `Ready` with reason `SizeUnknown` or `InsufficientCapacity`, phase `Failed`. The scratch PVC size is the resolved size.

- [ ] **Step 1: Write failing tests**

Cases: VolSync PVC missing and no target.size → phase Failed, reason `SizeUnknown`, no PVC created. target.size smaller than source → reason `InsufficientCapacity`, no PVC created. autoExpand set → PVC requests the computed size.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/controller/ -run TestRestoreSize -v`
Expected: FAIL.

- [ ] **Step 3: Wire the calls before PVC creation**

- [ ] **Step 4: Run tests**

Run: `go test ./internal/controller/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: fail restores on unknown or too small size"
```

### Task 5: Velero provider

**Files:**
- Create: `internal/controller/restore_velero.go`
- Create: `internal/controller/restore_velero_test.go`
- Modify: `internal/controller/provider_size.go` (replace the Velero stub)
- Modify: `internal/controller/restore_controller.go` (dispatch on provider)

**Interfaces:**
- Consumes: `sourceSize` dispatch (Task 3).
- Produces: `func (r *RestoreReconciler) reconcileVelero(ctx, restore) (ctrl.Result, error)`. Creates `velero.io/v1` Restore in namespace `velero` with `backupName`, `includedResources: [persistentvolumeclaims, persistentvolumes]`, `namespaceMapping: {<source ns>: bret-scratch-<restore name>}`, `restorePVs: true`. Watches its status (`status.phase`).

- [ ] **Step 1: Write failing unit test for the Velero Restore spec**

Assert the spec fields exactly as listed above.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/controller/ -run TestVeleroRestoreSpec -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

Use unstructured objects, the same way `restore_kopiur.go` does.

- [ ] **Step 4: Spike check before merge**

Run a manual check on the spike cluster: create a Velero Restore with the spec above, confirm the PVC lands in the scratch namespace and that PV names do not clash. Record the result in the PR body.

- [ ] **Step 5: Run tests and commit**

Run: `go test ./internal/controller/ -v`
Expected: PASS.

```bash
git add -A
git commit -m "feat: add Velero restore provider"
```

### Task 6: VolSync provider

**Files:**
- Create: `internal/controller/restore_volsync.go`
- Create: `internal/controller/restore_volsync_test.go`
- Modify: `internal/controller/restore_controller.go` (dispatch on provider)

**Interfaces:**
- Consumes: `sourceSize` (Task 3).
- Produces: `func (r *RestoreReconciler) reconcileVolSync(ctx, restore) (ctrl.Result, error)`. Creates a `ReplicationDestination` with `destinationPVC` set to the scratch PVC name, `copyMethod: Direct`, and `capacity` from the resolved size. Watches `status.latestImage`. Blueprint pod mounts the PVC after `latestImage` is ready.

- [ ] **Step 1: Write failing test**

Assert the ReplicationDestination spec: `destinationPVC`, `copyMethod: Direct`, `capacity`. Assert the Restore waits while `latestImage` is absent.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/controller/ -run TestVolSyncRestore -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

- [ ] **Step 4: Spike check before merge**

On the spike cluster: create a ReplicationDestination with `destinationPVC`, confirm `status.latestImage` reaches `PersistentVolumeClaim`. Note the `mover security context` is needed for rsync-tls (runAsUser/runAsGroup/fsGroup 65534) per the spike; record that the blueprint or chart sets it.

- [ ] **Step 5: Run tests and commit**

Run: `go test ./internal/controller/ -v`
Expected: PASS.

```bash
git add -A
git commit -m "feat: add VolSync restore provider"
```

### Task 7: Helm chart provider flags, RBAC and watchNamespaces

**Files:**
- Modify: `charts/bret-operator/values.yaml` (`providers.kopiur`, `providers.velero`, `providers.volsync` booleans; `watchNamespaces` list)
- Modify: `charts/bret-operator/templates/` (ClusterRole and Role templates gated by the flags; Roles per watched namespace)
- Modify: `cmd/main.go` (start controllers only for enabled providers; cache `ByObject` namespaces from `WATCH_NAMESPACES`)
- Modify: `charts/bret-operator/README.md` (regenerate with helm-docs)

**Interfaces:**
- Consumes: provider names from Task 5 and Task 6.
- Produces: env var `PROVIDERS` (comma list) and `WATCH_NAMESPACES` (comma list, empty = cluster-wide) passed into the manager.

- [ ] **Step 1: Write failing test for the chart render**

Run `helm template` with `providers.velero=false` and assert no `velero.io` rule renders. Expected: test script fails before the change.

- [ ] **Step 2: Implement templates and values**

- [ ] **Step 3: Implement flag parsing in `cmd/main.go` and cache config**

- [ ] **Step 4: Run chart render check**

Run: `helm template bret charts/bret-operator --set providers.velero=false | grep -c velero.io`
Expected: 0.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: gate providers and watched namespaces in chart"
```

### Task 8: RestoreTest watermark and volume mapping

**Files:**
- Modify: `api/v1alpha1/restoretest_types.go` (replace `HandledBackups []string` with `LastHandled *HandledBackup{Name string; CreatedAt metav1.Time}`)
- Modify: `api/v1alpha1/restoreblueprint_types.go` (`RestoredVolume` gets `SourcePVC string`)
- Modify: `internal/controller/restoretest_controller.go` (`afterBackupTrigger` compares against the watermark)
- Modify: `internal/controller/checkpod.go` (remove the `restoredVolumes is not supported yet` error; volume name from `RestoredVolume.Name`)
- Modify: `internal/controller/restoretest_reconcile_test.go` (replace `handledBackups` assertions)
- Test: `internal/controller/checkpod_test.go`

**Interfaces:**
- Consumes: none.
- Produces: `func isNewerThanWatermark(b backupRef, wm *HandledBackup) bool`. Compare `CreatedAt` then `Name` to break ties. Review Focus item 5 test lives here.

- [ ] **Step 1: Write failing tests**

Watermark: two backups with equal `CreatedAt`, names `a` and `b`; watermark `a` → `b` triggers. Volume mapping: blueprint with `restoredVolumes` and `sourcePVC` → check pod mounts the restored PVC under the volume name.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/controller/ -run 'TestWatermark|TestRestoredVolumes' -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

- [ ] **Step 4: Regenerate and run**

Run: `make manifests generate test`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: watermark backups and map restored volumes"
```

### Task 9: Docs and blueprint examples

**Files:**
- Modify: `docs/concepts.md`, `docs/getting-started.md`, `docs/restoretest-guide.md` (remove remaining "planned" markers for implemented features, document `autoExpand`, `SizeUnknown`, `InsufficientCapacity`, providers, `watchNamespaces`)
- Modify: `docs/backup-providers.md` (Velero and VolSync now implemented)
- Modify: `docs/README.md` (status table)
- Modify: `blueprints/nextcloud.yaml` (add `sourcePVC` to `restoredVolumes`)

**Interfaces:**
- Consumes: final field names from Tasks 2 to 8.

- [ ] **Step 1: Update docs**

Run: `grep -rn "planned\|need to clarify\|open" docs/ README.md`
Expected: only matches that describe real open items.

- [ ] **Step 2: Validate blueprints**

Run: `make test-blueprints` (or the blueprint test target in `hack/blueprint-test/`)
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add -A
git commit -m "docs: document size rule, providers and volume mapping"
```

### Task 10: Final verification

- [ ] **Step 1: Lint**

Run: `golangci-lint run`
Expected: `0 issues`.

- [ ] **Step 2: Unit and envtest**

Run: `make test`
Expected: PASS.

- [ ] **Step 3: Manager image build**

Run: `podman build --network host -t bret-operator:dev .` (docker daemon is down on this host; the Makefile default is still `docker`, open item)
Expected: image builds.

- [ ] **Step 4: Commit any fixes**

```bash
git add -A
git commit -m "fix: final verification fixes"
```
