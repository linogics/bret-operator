package controller

import (
	"context"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

func volsyncTestRestore() *restorev1alpha1.Restore {
	restore := newTestRestore()
	restore.Spec.Provider = restorev1alpha1.ProviderVolSync

	return restore
}

// volsyncSource is the ReplicationSource b1 of the backup, with its restic repository Secret.
func volsyncSource() *unstructured.Unstructured {
	src := &unstructured.Unstructured{}
	src.SetGroupVersionKind(replicationSourceGVK)
	src.SetNamespace(testNamespace)
	src.SetName("b1")
	_ = unstructured.SetNestedField(src.Object, testRepoSecret, "spec", "restic", "repository")

	return src
}

// volsyncSecret is the restic repository Secret in the Restore namespace.
func volsyncSecret() *corev1.Secret {
	return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: testRepoSecret, Namespace: testNamespace}}
}

func TestVolSyncRestoreSpec(t *testing.T) {
	restore := volsyncTestRestore()
	obj := volsyncDestinationObject(restore, restore.Spec.Sources[0], testRepoSecret, resource.MustParse("3Gi"), nil)

	if obj.GetNamespace() != testNamespace || obj.GetAPIVersion() != "volsync.backube/v1alpha1" || obj.GetKind() != "ReplicationDestination" {
		t.Fatalf("unexpected identity %s %s/%s", obj.GetAPIVersion(), obj.GetNamespace(), obj.GetKind())
	}

	want := map[string]any{
		"repository":     testRepoSecret,
		"copyMethod":     "Direct",
		"destinationPVC": restoredClaimName(restore, restore.Spec.Sources[0].SourcePVC),
		"capacity":       "3Gi",
		"moverSecurityContext": map[string]any{
			"runAsUser": int64(65534), "runAsGroup": int64(65534), "fsGroup": int64(65534),
		},
	}

	got, _, _ := unstructured.NestedMap(obj.Object, "spec", "restic")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("spec.restic = %v, want %v", got, want)
	}
}

func volsyncDestination(t *testing.T, c client.Client, restore *restorev1alpha1.Restore, srcs ...restorev1alpha1.RestoreSource) (*unstructured.Unstructured, error) {
	t.Helper()

	src := restore.Spec.Sources[0]
	if len(srcs) > 0 {
		src = srcs[0]
	}

	rd := volsyncStub(restore, src)

	return rd, c.Get(context.Background(), client.ObjectKeyFromObject(rd), rd)
}

func TestVolSyncRestoreWaitsOnLatestImage(t *testing.T) {
	ctx := context.Background()
	restore := volsyncTestRestore()
	c, r := fakeRestoreClient(t, restore, newTestCheck(), volsyncSource(), volsyncSecret())
	key := client.ObjectKeyFromObject(restore)

	reconcileToSizing(t, r, key) // Pending, then RD and PVC created

	rd, err := volsyncDestination(t, c, restore)
	if err != nil {
		t.Fatalf("ReplicationDestination not created: %v", err)
	}

	podKey := types.NamespacedName{Name: checkPodName(restore), Namespace: testNamespace}

	reconcileRestore(t, r, key)

	if err := c.Get(ctx, podKey, &corev1.Pod{}); !errors.IsNotFound(err) {
		t.Fatalf("check pod created before latestImage: %v", err)
	}

	if got := getRestore(t, c, key).Status.Phase; got != restorev1alpha1.RestorePhaseRunning {
		t.Fatalf("phase %q, want Running", got)
	}

	if err := unstructured.SetNestedField(rd.Object, "PersistentVolumeClaim", "status", "latestImage", "kind"); err != nil {
		t.Fatal(err)
	}

	if err := c.Update(ctx, rd); err != nil {
		t.Fatal(err)
	}

	reconcileRestore(t, r, key)

	pod := &corev1.Pod{}
	if err := c.Get(ctx, podKey, pod); err != nil {
		t.Fatalf("check pod not created after latestImage: %v", err)
	}

	if pod.Spec.Volumes[0].PersistentVolumeClaim.ClaimName != restoredClaimName(restore, restore.Spec.Sources[0].SourcePVC) {
		t.Fatalf("pod mounts %v, want the scratch PVC", pod.Spec.Volumes[0])
	}
}

func TestVolSyncRestoreWaitsForSource(t *testing.T) {
	restore := volsyncTestRestore()
	c, r := fakeRestoreClient(t, restore, newTestCheck())

	reconcileToSizing(t, r, client.ObjectKeyFromObject(restore))

	if _, err := volsyncDestination(t, c, restore); !errors.IsNotFound(err) {
		t.Fatalf("ReplicationDestination created without a ReplicationSource: %v", err)
	}

	if got := getRestore(t, c, client.ObjectKeyFromObject(restore)).Status.Phase; isTerminal(got) {
		t.Fatalf("phase %q, want a waiting phase", got)
	}
}

func TestVolSyncRestoreIsRouted(t *testing.T) {
	restore := volsyncTestRestore()
	c, r := fakeRestoreClient(t, restore, newTestCheck(), volsyncSource(), volsyncSecret())

	reconcileToSizing(t, r, client.ObjectKeyFromObject(restore))

	if reason := readyReason(getRestore(t, c, client.ObjectKeyFromObject(restore))); reason == reasonProviderUnsupported {
		t.Fatalf("VolSync restore is not routed: %s", reason)
	}
}

func TestVolSyncRestoreSizeUnknownFails(t *testing.T) {
	restore := volsyncTestRestore()
	restore.Spec.Sources[0].Size = nil
	c, r := fakeRestoreClient(t, restore, newTestCheck(), volsyncSource(), volsyncSecret()) // source PVC is missing

	reconcileToSizing(t, r, client.ObjectKeyFromObject(restore))
	assertSizeFailure(t, c, restore, restorev1alpha1.ReasonSizeUnknown)

	if _, err := volsyncDestination(t, c, restore); !errors.IsNotFound(err) {
		t.Errorf("ReplicationDestination created for a failed size: %v", err)
	}
}

func TestVolSyncSourceInBackupNamespace(t *testing.T) {
	restore := volsyncTestRestore()
	restore.Spec.Sources[0].BackupRef.Namespace = "backups"

	src := volsyncSource()
	src.SetNamespace("backups")
	_ = unstructured.SetNestedField(src.Object, testDataName, "spec", "sourcePVC")

	pvc := &corev1.PersistentVolumeClaim{}
	pvc.Namespace, pvc.Name = "backups", testDataName // none in the Restore namespace
	pvc.Spec.Resources.Requests = corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("2Gi")}

	_, r := fakeRestoreClient(t, restore, src, pvc, volsyncSecret())

	size, err := r.sourceSize(context.Background(), restore, restore.Spec.Sources[0])
	if err != nil || size == nil || size.Cmp(resource.MustParse("2Gi")) != 0 {
		t.Fatalf("sourceSize = %v, %v; want 2Gi", size, err)
	}

	repo, _, err := r.volsyncRepository(context.Background(), restore, restore.Spec.Sources[0])
	if err != nil || repo != testRepoSecret {
		t.Fatalf("volsyncRepository = %q, %v; want repo-secret", repo, err)
	}
}

func twoVolSyncSources() (restore *restorev1alpha1.Restore, objs []client.Object) {
	restore = twoSourceRestore()
	restore.Spec.Provider = restorev1alpha1.ProviderVolSync

	for i, pvc := range []string{testHTML, testPGData} {
		rs := volsyncSource()
		rs.SetName(restore.Spec.Sources[i].BackupRef.Name)
		_ = unstructured.SetNestedField(rs.Object, pvc, "spec", "sourcePVC")
		objs = append(objs, rs)
	}

	return restore, append(objs, volsyncSecret())
}

func setLatestImage(t *testing.T, c client.Client, restore *restorev1alpha1.Restore, src restorev1alpha1.RestoreSource) {
	t.Helper()

	rd := volsyncStub(restore, src)
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(rd), rd); err != nil {
		t.Fatal(err)
	}

	if err := unstructured.SetNestedField(rd.Object, "PersistentVolumeClaim", "status", "latestImage", "kind"); err != nil {
		t.Fatal(err)
	}

	if err := c.Update(context.Background(), rd); err != nil {
		t.Fatal(err)
	}
}

func TestVolSyncTwoSources(t *testing.T) {
	ctx := context.Background()
	restore, objs := twoVolSyncSources()
	c, r := fakeRestoreClient(t, append(objs, restore, twoVolumeCheck())...)
	key := client.ObjectKeyFromObject(restore)

	reconcileToSizing(t, r, key)

	for _, pvc := range []string{testHTML, testPGData} {
		rd := &unstructured.Unstructured{}
		rd.SetGroupVersionKind(replicationDestinationGVK)

		if err := c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "r1-volsync-" + pvc}, rd); err != nil {
			t.Fatalf("RD of %s: %v", pvc, err)
		}

		if got, _, _ := unstructured.NestedString(rd.Object, "spec", "restic", "destinationPVC"); got != "r1-"+pvc {
			t.Fatalf("destinationPVC = %q, want r1-%s", got, pvc)
		}

		if err := c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "r1-" + pvc}, &corev1.PersistentVolumeClaim{}); err != nil {
			t.Fatalf("PVC of %s: %v", pvc, err)
		}
	}

	if refs := getRestore(t, c, key).Status.ProviderRefs; len(refs) != 2 {
		t.Fatalf("providerRefs = %v, want 2", refs)
	}

	podKey := client.ObjectKey{Namespace: testNamespace, Name: checkPodName(restore)}

	setLatestImage(t, c, restore, restore.Spec.Sources[0])
	reconcileRestore(t, r, key)

	if err := c.Get(ctx, podKey, &corev1.Pod{}); !errors.IsNotFound(err) {
		t.Fatalf("check pod with one destination pending: %v", err)
	}

	setLatestImage(t, c, restore, restore.Spec.Sources[1])
	reconcileRestore(t, r, key)

	if err := c.Get(ctx, podKey, &corev1.Pod{}); err != nil {
		t.Fatalf("check pod not created: %v", err)
	}
}

func TestVolSyncSourcePVCMismatch(t *testing.T) {
	restore, objs := twoVolSyncSources()
	_ = unstructured.SetNestedField(objs[1].(*unstructured.Unstructured).Object, testOther, "spec", "sourcePVC")

	c, r := fakeRestoreClient(t, append(objs, restore, twoVolumeCheck())...)
	key := client.ObjectKeyFromObject(restore)

	reconcileToSizing(t, r, key)

	got := getRestore(t, c, key)
	if got.Status.Phase != restorev1alpha1.RestorePhaseFailed || readyReason(got) != reasonSourceMismatch {
		t.Fatalf("phase %q reason %q, want Failed %s", got.Status.Phase, readyReason(got), reasonSourceMismatch)
	}

	for _, src := range restore.Spec.Sources {
		if _, err := volsyncDestination(t, c, restore, src); !errors.IsNotFound(err) {
			t.Fatalf("ReplicationDestination of %s created: %v", src.SourcePVC, err)
		}
	}
}

func TestVolSyncMoverUIDPerSource(t *testing.T) {
	restore, objs := twoVolSyncSources()
	bp := twoVolumeCheck()
	bp.Annotations = map[string]string{moverUIDAnnotation: `{"pgdata":999}`}
	bp.Spec.Containers[0].SecurityContext = nil // no runAsUser fallback

	c, r := fakeRestoreClient(t, append(objs, restore, bp)...)

	reconcileToSizing(t, r, client.ObjectKeyFromObject(restore))

	for i, want := range []int64{65534, 999} {
		rd, err := volsyncDestination(t, c, restore, restore.Spec.Sources[i])
		if err != nil {
			t.Fatal(err)
		}

		if got, _, _ := unstructured.NestedInt64(rd.Object, "spec", "restic", "moverSecurityContext", "runAsUser"); got != want {
			t.Fatalf("%s runAsUser = %d, want %d", restore.Spec.Sources[i].SourcePVC, got, want)
		}
	}
}

func TestCleanupDeletesAllVolSyncDestinations(t *testing.T) {
	restore, objs := twoVolSyncSources()
	c, r := fakeRestoreClient(t, append(objs, restore, twoVolumeCheck())...)

	for _, src := range restore.Spec.Sources {
		if err := c.Create(context.Background(), volsyncDestinationObject(restore, src, testRepoSecret, resource.MustParse("1Gi"), nil)); err != nil {
			t.Fatal(err)
		}
	}

	if err := r.cleanup(context.Background(), getRestore(t, c, client.ObjectKeyFromObject(restore))); err != nil {
		t.Fatal(err)
	}

	for _, src := range restore.Spec.Sources {
		if _, err := volsyncDestination(t, c, restore, src); !errors.IsNotFound(err) {
			t.Fatalf("ReplicationDestination of %s remains: %v", src.SourcePVC, err)
		}
	}
}
