package controller

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
		backups, err = r.listBackups(ctx, snapshotGVK, test.Namespace, func(list []unstructured.Unstructured) ([]completedBackup, error) {
			return kopiurBackups(list, sel)
		})
	case restorev1alpha1.ProviderVolSync:
		backups, err = r.listBackups(ctx, replicationSourceGVK, test.Namespace, func(list []unstructured.Unstructured) ([]completedBackup, error) {
			return volsyncBackups(list, sel)
		})
	case restorev1alpha1.ProviderVelero:
		backups, err = r.listBackups(ctx, veleroBackupGVK, r.veleroNS(), func(list []unstructured.Unstructured) ([]completedBackup, error) {
			return veleroBackups(list, sel, test.Namespace), nil
		})
	default:
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	sortBackups(backups)

	return backups, nil
}

// kopiurBackups keeps Succeeded Kopiur Snapshots. Completion is the Ready condition time.
func kopiurBackups(list []unstructured.Unstructured, sel restorev1alpha1.BackupSelector) ([]completedBackup, error) {
	var backups []completedBackup

	for i := range list {
		obj := &list[i]

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

// listBackups lists objects of one kind in a namespace and turns them into backups.
func (r *RestoreTestReconciler) listBackups(
	ctx context.Context,
	gvk schema.GroupVersionKind,
	namespace string,
	pick func([]unstructured.Unstructured) ([]completedBackup, error),
) ([]completedBackup, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk.GroupVersion().WithKind(gvk.Kind + "List"))

	if err := r.reader().List(ctx, list, client.InNamespace(namespace)); err != nil {
		return nil, err
	}

	return pick(list.Items)
}

// errBackupSelectorAmbiguous means a VolSync selector matched more than one ReplicationSource.
var errBackupSelectorAmbiguous = errors.New("backupSelector matches more than one ReplicationSource")

// volsyncBackups treats each synced ReplicationSource as one backup that completed at status.lastSyncTime.
// VolSync keeps one ReplicationSource per PVC, so a selector must match exactly one.
func volsyncBackups(list []unstructured.Unstructured, sel restorev1alpha1.BackupSelector) ([]completedBackup, error) {
	var (
		backups []completedBackup
		matched []string
	)

	for i := range list {
		obj := &list[i]
		if !selects(sel, obj) {
			continue
		}

		matched = append(matched, obj.GetName())

		raw, _, _ := unstructured.NestedString(obj.Object, "status", "lastSyncTime")

		synced, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			continue
		}

		backups = append(backups, completedBackup{name: obj.GetName(), completed: synced})
	}

	if len(matched) > 1 {
		sort.Strings(matched)

		return nil, fmt.Errorf("%w: %s", errBackupSelectorAmbiguous, strings.Join(matched, ", "))
	}

	return backups, nil
}

// veleroBackups keeps Completed Velero Backups that cover the namespace. Completion is status.completionTimestamp.
func veleroBackups(list []unstructured.Unstructured, sel restorev1alpha1.BackupSelector, namespace string) []completedBackup {
	var backups []completedBackup

	for i := range list {
		obj := &list[i]

		phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
		if phase != veleroPhaseCompleted || !selects(sel, obj) || !coversNamespace(obj, namespace) {
			continue
		}

		raw, _, _ := unstructured.NestedString(obj.Object, "status", "completionTimestamp")

		done, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			continue
		}

		backups = append(backups, completedBackup{name: obj.GetName(), completed: done})
	}

	return backups
}

// coversNamespace reports whether a Velero Backup includes the namespace. No includedNamespaces means all.
func coversNamespace(obj *unstructured.Unstructured, namespace string) bool {
	excluded, _, _ := unstructured.NestedStringSlice(obj.Object, "spec", "excludedNamespaces")
	if slices.Contains(excluded, namespace) {
		return false
	}

	included, _, _ := unstructured.NestedStringSlice(obj.Object, "spec", "includedNamespaces")

	return len(included) == 0 || slices.Contains(included, "*") || slices.Contains(included, namespace)
}

// veleroNS is the namespace that holds Velero Backups.
func (r *RestoreTestReconciler) veleroNS() string {
	if r.VeleroNamespace == "" {
		return defaultVeleroNamespace
	}

	return r.VeleroNamespace
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
