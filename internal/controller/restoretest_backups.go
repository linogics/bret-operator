package controller

import (
	"context"
	"sort"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

// completedBackups lists the finished backups of the test provider that match sel, oldest first.
// Equal times are ordered by name.
func (r *RestoreTestReconciler) completedBackups(
	ctx context.Context,
	test *restorev1alpha1.RestoreTest,
	sel restorev1alpha1.BackupSelector,
) ([]completedBackup, error) {
	var (
		backups []completedBackup
		err     error
	)

	switch test.Spec.Provider {
	case restorev1alpha1.ProviderKopiur:
		backups, err = r.kopiurBackups(ctx, test.Namespace, sel)
	default:
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	sortBackups(backups)

	return backups, nil
}

// kopiurBackups lists Succeeded Kopiur Snapshots in the namespace. Completion is the Ready condition time.
func (r *RestoreTestReconciler) kopiurBackups(ctx context.Context, namespace string, sel restorev1alpha1.BackupSelector) ([]completedBackup, error) {
	snapshots := &unstructured.UnstructuredList{}
	snapshots.SetGroupVersionKind(snapshotGVK.GroupVersion().WithKind("SnapshotList"))

	if err := r.reader().List(ctx, snapshots, client.InNamespace(namespace)); err != nil {
		return nil, err
	}

	var backups []completedBackup

	for i := range snapshots.Items {
		obj := &snapshots.Items[i]

		if kopiurPhase(obj) != snapshotPhaseSucceeded || !selects(sel, obj) {
			continue
		}

		completed, ok := completionTime(obj)
		if !ok {
			continue
		}

		backups = append(backups, completedBackup{name: obj.GetName(), completed: completed})
	}

	return backups, nil
}

// sortBackups orders backups oldest first, ties by name.
func sortBackups(backups []completedBackup) {
	sort.Slice(backups, func(i, j int) bool {
		if backups[i].completed.Equal(backups[j].completed) {
			return backups[i].name < backups[j].name
		}

		return backups[i].completed.Before(backups[j].completed)
	})
}

// handled is the backup as picked for one source. Its completion time pins VolSync restores.
func (b completedBackup) handled(sourcePVC string) restorev1alpha1.HandledBackup {
	return restorev1alpha1.HandledBackup{SourcePVC: sourcePVC, Name: b.name, CreatedAt: metav1.NewTime(b.completed)}
}

// selects reports whether a backup object matches the backup selector.
func selects(sel restorev1alpha1.BackupSelector, obj *unstructured.Unstructured) bool {
	if sel.Name != "" {
		return obj.GetName() == sel.Name
	}

	if len(sel.Labels) == 0 {
		return false
	}

	for k, v := range sel.Labels {
		if obj.GetLabels()[k] != v {
			return false
		}
	}

	return true
}

// completionTime is the lastTransitionTime of the Ready=True condition of a Kopiur Snapshot.
func completionTime(obj *unstructured.Unstructured) (time.Time, bool) {
	conditions, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")

	for _, c := range conditions {
		condition, ok := c.(map[string]any)
		if !ok || condition["type"] != "Ready" || condition["status"] != "True" {
			continue
		}

		raw, _ := condition["lastTransitionTime"].(string)

		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return time.Time{}, false
		}

		return t, true
	}

	return time.Time{}, false
}
