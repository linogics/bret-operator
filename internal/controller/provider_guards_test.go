package controller

import (
	"context"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

func veleroBackup(snapshotVolumes *bool) *unstructured.Unstructured {
	b := &unstructured.Unstructured{}
	b.SetGroupVersionKind(veleroBackupGVK)
	b.SetNamespace(defaultVeleroNamespace)
	b.SetName("b1")

	if snapshotVolumes != nil {
		_ = unstructured.SetNestedField(b.Object, *snapshotVolumes, "spec", "snapshotVolumes")
	}

	return b
}

// fsBackupOf is a Velero PodVolumeBackup of one PVC, as velero pkg/podvolume/backupper.go writes it.
func fsBackupOf(pvc string) *unstructured.Unstructured {
	pvb := &unstructured.Unstructured{}
	pvb.SetAPIVersion("velero.io/v1")
	pvb.SetKind("PodVolumeBackup")
	pvb.SetNamespace(defaultVeleroNamespace)
	pvb.SetName("b1-xyz")
	pvb.SetLabels(map[string]string{"velero.io/backup-name": "b1"})
	pvb.SetAnnotations(map[string]string{"velero.io/pvc-name": pvc})
	_ = unstructured.SetNestedField(pvb.Object, testNamespace, "spec", "tags", "ns")

	return pvb
}

func TestVeleroRefusesBackupWithoutVolumeData(t *testing.T) {
	off := false

	for _, tt := range []struct {
		name string
		objs []client.Object
		want string
	}{
		{"file system backup", []client.Object{veleroBackup(nil), fsBackupOf(testDataName)}, "file system backup"},
		{"snapshotVolumes false", []client.Object{veleroBackup(&off)}, "snapshotVolumes false"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			restore := newTestRestore()
			restore.Spec.Provider = restorev1alpha1.ProviderVelero
			restore.Spec.Sources[0].BackupRef.Name = "b1"

			c, r := fakeRestoreClient(t, append(tt.objs, restore, newTestCheck())...)
			key := client.ObjectKeyFromObject(restore)

			reconcileRestore(t, r, key)
			reconcileRestore(t, r, key)

			got := getRestore(t, c, key)
			if got.Status.Phase != restorev1alpha1.RestorePhaseFailed || !strings.Contains(got.Status.Message, tt.want) {
				t.Fatalf("phase %q message %q, want Failed with %q", got.Status.Phase, got.Status.Message, tt.want)
			}

			if err := c.Get(context.Background(), client.ObjectKeyFromObject(r.veleroStub(restore)), r.veleroStub(restore)); err == nil {
				t.Fatal("Velero restore created for a backup it cannot restore")
			}
		})
	}
}

func TestVeleroAcceptsBackupOfOtherPVC(t *testing.T) {
	restore := newTestRestore()
	restore.Spec.Provider = restorev1alpha1.ProviderVelero
	restore.Spec.Sources[0].BackupRef.Name = "b1"

	c, r := fakeRestoreClient(t, veleroBackup(nil), fsBackupOf("other"), restore, newTestCheck())
	key := client.ObjectKeyFromObject(restore)

	reconcileRestore(t, r, key)
	reconcileRestore(t, r, key)

	if err := c.Get(context.Background(), client.ObjectKeyFromObject(r.veleroStub(restore)), r.veleroStub(restore)); err != nil {
		t.Fatalf("Velero restore not created: %v", err)
	}
}

func TestVeleroWaitsOnDyingVeleroRestore(t *testing.T) {
	ctx := context.Background()
	c, r, restore := completedVelero(t, newTestCheck())

	vr := r.veleroStub(restore)
	if err := c.Get(ctx, client.ObjectKeyFromObject(vr), vr); err != nil {
		t.Fatal(err)
	}

	// Velero holds its Restore with a finalizer while it deletes it.
	vr.SetFinalizers([]string{"restores.velero.io/external-resources-finalizer"})

	if err := c.Update(ctx, vr); err != nil {
		t.Fatal(err)
	}

	if err := c.Delete(ctx, vr); err != nil {
		t.Fatal(err)
	}

	key := client.ObjectKeyFromObject(restore)
	reconcileRestore(t, r, key)

	if got := getRestore(t, c, key).Status.Phase; got != restorev1alpha1.RestorePhaseRunning {
		t.Fatalf("phase %q, want Running until the old Velero restore is gone", got)
	}
}

func TestVolSyncNoEligibleSnapshotFails(t *testing.T) {
	ctx := context.Background()
	restore := volsyncTestRestore()
	c, r := fakeRestoreClient(t, restore, newTestCheck(), volsyncSource(), volsyncSecret())
	key := client.ObjectKeyFromObject(restore)

	reconcileToSizing(t, r, key)

	rd, err := volsyncDestination(t, c, restore)
	if err != nil {
		t.Fatal(err)
	}

	// What VolSync 0.17 stores when the restic mover finds no snapshot at or before restoreAsOf.
	_ = unstructured.SetNestedField(rd.Object, "PersistentVolumeClaim", "status", "latestImage", "kind")
	_ = unstructured.SetNestedField(rd.Object, "No eligible snapshots found\n=== No data will be restored ===", "status", "latestMoverStatus", "logs")

	if err := c.Update(ctx, rd); err != nil {
		t.Fatal(err)
	}

	reconcileRestore(t, r, key)

	if got := getRestore(t, c, key).Status.Phase; got != restorev1alpha1.RestorePhaseFailed {
		t.Fatalf("phase %q, want Failed: the restored volume is empty", got)
	}
}
