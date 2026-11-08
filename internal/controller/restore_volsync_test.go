package controller

import (
	"context"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
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
	_ = unstructured.SetNestedField(src.Object, "repo-secret", "spec", "restic", "repository")

	return src
}

func TestVolSyncRestoreSpec(t *testing.T) {
	restore := volsyncTestRestore()
	obj := volsyncDestinationObject(restore, "repo-secret", resource.MustParse("3Gi"), nil)

	if obj.GetNamespace() != testNamespace || obj.GetAPIVersion() != "volsync.backube/v1alpha1" || obj.GetKind() != "ReplicationDestination" {
		t.Fatalf("unexpected identity %s %s/%s", obj.GetAPIVersion(), obj.GetNamespace(), obj.GetKind())
	}

	want := map[string]any{
		"repository":     "repo-secret",
		"copyMethod":     "Direct",
		"destinationPVC": restoredClaimName(restore),
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

func volsyncDestination(t *testing.T, c client.Client, restore *restorev1alpha1.Restore) (*unstructured.Unstructured, error) {
	t.Helper()

	rd := volsyncStub(restore)

	return rd, c.Get(context.Background(), client.ObjectKeyFromObject(rd), rd)
}

func TestVolSyncRestoreWaitsOnLatestImage(t *testing.T) {
	ctx := context.Background()
	restore := volsyncTestRestore()
	c, r := fakeRestoreClient(t, restore, newTestBlueprint(), volsyncSource())
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

	if pod.Spec.Volumes[0].PersistentVolumeClaim.ClaimName != restoredClaimName(restore) {
		t.Fatalf("pod mounts %v, want the scratch PVC", pod.Spec.Volumes[0])
	}
}

func TestVolSyncRestoreWaitsForSource(t *testing.T) {
	restore := volsyncTestRestore()
	c, r := fakeRestoreClient(t, restore, newTestBlueprint())

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
	c, r := fakeRestoreClient(t, restore, newTestBlueprint(), volsyncSource())

	reconcileToSizing(t, r, client.ObjectKeyFromObject(restore))

	if reason := readyReason(getRestore(t, c, client.ObjectKeyFromObject(restore))); reason == reasonProviderUnsupported {
		t.Fatalf("VolSync restore is not routed: %s", reason)
	}
}

func TestVolSyncRestoreSizeUnknownFails(t *testing.T) {
	restore := volsyncTestRestore()
	restore.Spec.Target.Size = nil
	c, r := fakeRestoreClient(t, restore, newTestBlueprint(), volsyncSource()) // source PVC is missing

	reconcileToSizing(t, r, client.ObjectKeyFromObject(restore))
	assertSizeFailure(t, c, restore, restorev1alpha1.ReasonSizeUnknown)

	if _, err := volsyncDestination(t, c, restore); !errors.IsNotFound(err) {
		t.Errorf("ReplicationDestination created for a failed size: %v", err)
	}
}

func TestVolSyncSourceInBackupNamespace(t *testing.T) {
	restore := volsyncTestRestore()
	restore.Spec.Source.BackupRef.Namespace = "backups"

	src := volsyncSource()
	src.SetNamespace("backups")
	_ = unstructured.SetNestedField(src.Object, testDataName, "spec", "sourcePVC")

	pvc := &corev1.PersistentVolumeClaim{}
	pvc.Namespace, pvc.Name = "backups", testDataName // none in the Restore namespace
	pvc.Spec.Resources.Requests = corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("2Gi")}

	_, r := fakeRestoreClient(t, restore, src, pvc)

	size, err := r.sourceSize(context.Background(), restore)
	if err != nil || size == nil || size.Cmp(resource.MustParse("2Gi")) != 0 {
		t.Fatalf("sourceSize = %v, %v; want 2Gi", size, err)
	}

	repo, err := r.volsyncRepository(context.Background(), restore)
	if err != nil || repo != "repo-secret" {
		t.Fatalf("volsyncRepository = %q, %v; want repo-secret", repo, err)
	}
}
