package controller

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

const (
	testHTML   = "html"
	testPGData = "pgdata"
)

// twoSourceRestore restores html (b1) and pgdata (b2) with Kopiur.
func twoSourceRestore() *restorev1alpha1.Restore {
	restore := newTestRestore()
	restore.Spec.Sources = []restorev1alpha1.RestoreSource{
		{SourcePVC: testHTML, BackupRef: restorev1alpha1.BackupRef{Name: "b1"}, RestoreTarget: restorev1alpha1.RestoreTarget{Size: quantityPtr(testSize1Gi)}},
		{SourcePVC: testPGData, BackupRef: restorev1alpha1.BackupRef{Name: "b2"}, RestoreTarget: restorev1alpha1.RestoreTarget{Size: quantityPtr(testSize1Gi)}},
	}

	return restore
}

func kopiurSnapshotFor(name string, sizeBytes int64, pvc string) *unstructured.Unstructured {
	snap := &unstructured.Unstructured{}
	snap.SetGroupVersionKind(snapshotGVK)
	snap.SetNamespace(testNamespace)
	snap.SetName(name)

	if sizeBytes > 0 {
		_ = unstructured.SetNestedField(snap.Object, sizeBytes, "status", "stats", "sizeBytes")
	}

	if pvc != "" {
		_ = unstructured.SetNestedSlice(snap.Object, []any{map[string]any{testPVCKey: pvc}}, "status", "resolved", "sources")
	}

	return snap
}

func TestMatchSourcesMissing(t *testing.T) {
	restore := newTestRestore()
	restore.Spec.Sources[0].SourcePVC = testHTML

	bp := twoVolumeCheck()

	if err := matchSources(restore, bp); err == nil || !strings.Contains(err.Error(), testPGData) {
		t.Fatalf("missing source: err = %v, want it to name %s", err, testPGData)
	}

	restore = twoSourceRestore()
	bp.Spec.RestoredVolumes = bp.Spec.RestoredVolumes[:1]

	if err := matchSources(restore, bp); err == nil || !strings.Contains(err.Error(), testPGData) {
		t.Fatalf("extra source: err = %v, want it to name %s", err, testPGData)
	}

	if err := matchSources(twoSourceRestore(), twoVolumeCheck()); err != nil {
		t.Fatalf("matching sources: %v", err)
	}

	restore.Spec.Provider = restorev1alpha1.ProviderVelero
	if err := matchSources(restore, bp); err != nil {
		t.Fatalf("velero: %v", err)
	}
}

func TestRestoreSourceMissingFails(t *testing.T) {
	restore := newTestRestore()
	restore.Spec.Sources[0].SourcePVC = testHTML
	c, r := fakeRestoreClient(t, restore, twoVolumeCheck())
	key := client.ObjectKeyFromObject(restore)

	reconcileToSizing(t, r, key)

	got := getRestore(t, c, key)
	if got.Status.Phase != restorev1alpha1.RestorePhaseFailed || readyReason(got) != reasonSourceMissing {
		t.Fatalf("phase %q reason %q, want Failed %s", got.Status.Phase, readyReason(got), reasonSourceMissing)
	}

	if names := kopiurRestoreNames(t, c); len(names) != 0 {
		t.Fatalf("Kopiur objects created: %v", names)
	}
}

func kopiurRestoreNames(t *testing.T, c client.Client) []string {
	t.Helper()

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(kopiurRestoreGVK.GroupVersion().WithKind("RestoreList"))

	if err := c.List(context.Background(), list); err != nil {
		t.Fatal(err)
	}

	names := make([]string, 0, len(list.Items))
	for i := range list.Items {
		names = append(names, list.Items[i].GetName())
	}

	return names
}

func setKopiurPhase(t *testing.T, c client.Client, restore *restorev1alpha1.Restore, pvc, phase string) {
	t.Helper()

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(kopiurRestoreGVK)

	key := client.ObjectKey{Namespace: restore.Namespace, Name: kopiurName(restore, pvc)}
	if err := c.Get(context.Background(), key, obj); err != nil {
		t.Fatal(err)
	}

	if err := unstructured.SetNestedField(obj.Object, phase, "status", "phase"); err != nil {
		t.Fatal(err)
	}

	if err := c.Update(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
}

func TestKopiurTwoSources(t *testing.T) {
	ctx := context.Background()
	restore := twoSourceRestore()
	c, r := fakeRestoreClient(t, restore, twoVolumeCheck())
	key := client.ObjectKeyFromObject(restore)

	reconcileToSizing(t, r, key)

	for _, pvc := range []string{testHTML, testPGData} {
		kopiur := &unstructured.Unstructured{}
		kopiur.SetGroupVersionKind(kopiurRestoreGVK)

		if err := c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "r1-kopiur-" + pvc}, kopiur); err != nil {
			t.Fatalf("Kopiur restore of %s: %v", pvc, err)
		}

		claim := &corev1.PersistentVolumeClaim{}
		if err := c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "r1-" + pvc}, claim); err != nil {
			t.Fatalf("PVC of %s: %v", pvc, err)
		}

		if ref := claim.Spec.DataSourceRef; ref == nil || ref.Name != "r1-kopiur-"+pvc {
			t.Fatalf("PVC %s dataSourceRef = %v", pvc, ref)
		}
	}

	if refs := getRestore(t, c, key).Status.ProviderRefs; len(refs) != 2 {
		t.Fatalf("providerRefs = %v, want 2", refs)
	}

	podKey := client.ObjectKey{Namespace: testNamespace, Name: checkPodName(restore)}

	setKopiurPhase(t, c, restore, testHTML, kopiurPhaseCompleted)
	reconcileRestore(t, r, key)

	if err := c.Get(ctx, podKey, &corev1.Pod{}); !errors.IsNotFound(err) {
		t.Fatalf("check pod with one restore running: %v", err)
	}

	setKopiurPhase(t, c, restore, testPGData, kopiurPhaseCompleted)
	reconcileRestore(t, r, key)

	pod := &corev1.Pod{}
	if err := c.Get(ctx, podKey, pod); err != nil {
		t.Fatalf("check pod not created: %v", err)
	}

	claims := map[string]bool{}

	for _, v := range pod.Spec.Volumes {
		if v.PersistentVolumeClaim != nil {
			claims[v.PersistentVolumeClaim.ClaimName] = true
		}
	}

	if !claims["r1-html"] || !claims["r1-pgdata"] {
		t.Fatalf("pod claims = %v, want r1-html and r1-pgdata", claims)
	}
}

func TestKopiurOneFailedFailsAttempt(t *testing.T) {
	restore := twoSourceRestore()
	c, r := fakeRestoreClient(t, restore, twoVolumeCheck())
	key := client.ObjectKeyFromObject(restore)

	reconcileToSizing(t, r, key)
	setKopiurPhase(t, c, restore, testPGData, kopiurPhaseFailed)
	reconcileRestore(t, r, key)

	if got := getRestore(t, c, key).Status.Phase; got != restorev1alpha1.RestorePhaseFailed {
		t.Fatalf("phase %q, want Failed", got)
	}
}

func TestKopiurSnapshotPVCMismatch(t *testing.T) {
	restore := newTestRestore()
	restore.Spec.Sources[0].SourcePVC = testHTML

	bp := newTestCheck()
	bp.Spec.RestoredVolumes[0].SourcePVC = testHTML

	c, r := fakeRestoreClient(t, restore, bp, kopiurSnapshotFor("b1", 0, "data/other"))
	key := client.ObjectKeyFromObject(restore)

	reconcileToSizing(t, r, key)

	got := getRestore(t, c, key)
	if got.Status.Phase != restorev1alpha1.RestorePhaseFailed || readyReason(got) != reasonSourceMismatch {
		t.Fatalf("phase %q reason %q, want Failed %s", got.Status.Phase, readyReason(got), reasonSourceMismatch)
	}

	if !strings.Contains(got.Status.Message, testOther) || !strings.Contains(got.Status.Message, testHTML) {
		t.Fatalf("message %q must name both PVCs", got.Status.Message)
	}

	if names := kopiurRestoreNames(t, c); len(names) != 0 {
		t.Fatalf("Kopiur objects created: %v", names)
	}
}

func TestKopiurSnapshotPVC(t *testing.T) {
	snap := func(fields map[string]any) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: fields}
	}
	policy := snap(map[string]any{testSpecKey: map[string]any{"sources": []any{map[string]any{testPVCKey: map[string]any{keyName: "from-policy"}}}}})

	tests := []struct {
		name         string
		snap, policy *unstructured.Unstructured
		want         string
	}{
		{
			"resolved",
			snap(map[string]any{"status": map[string]any{"resolved": map[string]any{"sources": []any{map[string]any{testPVCKey: "ns/from-status"}}}}}),
			policy, "from-status",
		},
		{
			"fanned out",
			snap(map[string]any{testSpecKey: map[string]any{"source": map[string]any{"target": map[string]any{testPVCKey: map[string]any{"name": "from-spec"}}}}}),
			policy, "from-spec",
		},
		{"policy", snap(map[string]any{}), policy, "from-policy"},
		{"unknown", snap(map[string]any{}), nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := kopiurSnapshotPVC(tt.snap, tt.policy); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestKopiurSnapshotPVCFromPolicy(t *testing.T) {
	restore := newTestRestore()
	restore.Spec.Sources[0].SourcePVC = testHTML

	bp := newTestCheck()
	bp.Spec.RestoredVolumes[0].SourcePVC = testHTML

	snap := kopiurSnapshotFor("b1", 0, "")
	_ = unstructured.SetNestedField(snap.Object, "pol", testSpecKey, "policyRef", "name")

	policy := &unstructured.Unstructured{}
	policy.SetGroupVersionKind(snapshotPolicyGVK)
	policy.SetNamespace(testNamespace)
	policy.SetName("pol")
	_ = unstructured.SetNestedSlice(policy.Object, []any{map[string]any{testPVCKey: map[string]any{keyName: testOther}}}, testSpecKey, "sources")

	c, r := fakeRestoreClient(t, restore, bp, snap, policy)
	key := client.ObjectKeyFromObject(restore)

	reconcileToSizing(t, r, key)

	if got := readyReason(getRestore(t, c, key)); got != reasonSourceMismatch {
		t.Fatalf("reason %q, want %s", got, reasonSourceMismatch)
	}
}

func TestSourceSizesPerSource(t *testing.T) {
	restore := twoSourceRestore()
	restore.Spec.Sources[0].Size = quantityPtr(testSize5Gi)
	restore.Spec.Sources[1].Size = nil

	_, r := fakeRestoreClient(t, restore, twoVolumeCheck(), kopiurSnapshotFor("b2", 10<<30, ""))

	sizes, err := r.sourceSizes(context.Background(), restore)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]string{testHTML: testSize5Gi, testPGData: "11Gi"}
	for pvc, w := range want {
		got := sizes[pvc]
		if got.Cmp(resource.MustParse(w)) != 0 {
			t.Errorf("size of %s = %s, want %s", pvc, got.String(), w)
		}

		stored := restore.Status.ResolvedSizes[pvc]
		if stored.Cmp(resource.MustParse(w)) != 0 {
			t.Errorf("stored size of %s = %s, want %s", pvc, stored.String(), w)
		}
	}
}

func TestCleanupDeletesAllKopiurRestores(t *testing.T) {
	restore := twoSourceRestore()
	c, r := fakeRestoreClient(t, restore, twoVolumeCheck())

	for _, src := range restore.Spec.Sources {
		if err := c.Create(context.Background(), kopiurRestoreObject(restore, src, nil)); err != nil {
			t.Fatal(err)
		}
	}

	foreign := kopiurRestoreObject(restore, restore.Spec.Sources[0], nil)
	foreign.SetName("foreign")
	foreign.SetLabels(map[string]string{restoreLabel: "someone-else"})

	if err := c.Create(context.Background(), foreign); err != nil {
		t.Fatal(err)
	}

	stored := getRestore(t, c, client.ObjectKeyFromObject(restore))
	if err := r.cleanup(context.Background(), stored); err != nil {
		t.Fatal(err)
	}

	if names := kopiurRestoreNames(t, c); len(names) != 1 || names[0] != "foreign" {
		t.Fatalf("remaining Kopiur restores = %v, want [foreign]", names)
	}
}
