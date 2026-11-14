package controller

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

const (
	kopiurGroup          = "kopiur.home-operations.com"
	kopiurPhaseCompleted = "Completed"
	kopiurPhaseFailed    = "Failed"
	moverUIDAnnotation   = "bret.linogics.io/mover-uid"
	keyName              = "name"
	keyNamespace         = "namespace"
)

// kopiurRestoreGVK is the Kopiur Restore kind. It is used as unstructured. Kopiur is not a Go dependency.
var kopiurRestoreGVK = schema.GroupVersionKind{Group: kopiurGroup, Version: apiVersionAlpha1, Kind: "Restore"}

// kopiurName is the name of the Kopiur Restore of one attempt.
func kopiurName(restore *restorev1alpha1.Restore) string {
	return restore.Name + "-kopiur"
}

// backupNamespace is the namespace of the backup object. It is backupRef.namespace, or else the Restore namespace.
func backupNamespace(restore *restorev1alpha1.Restore) string {
	if ns := restore.Spec.Source.BackupRef.Namespace; ns != "" {
		return ns
	}

	return restore.Namespace
}

// restoredClaimName is the PVC that receives the restored data.
func restoredClaimName(restore *restorev1alpha1.Restore) string {
	return restore.Name + "-scratch"
}

// scratchClaimName is the PVC of one check scratch volume.
func scratchClaimName(restore *restorev1alpha1.Restore, volume string) string {
	return restore.Name + "-scratch-" + dnsLabel(volume)
}

// restoreLabels labels every object that an attempt creates. Cleanup uses them to find the objects.
func restoreLabels(restore *restorev1alpha1.Restore) map[string]string {
	return map[string]string{restoreLabel: restore.Name}
}

// kopiurStub names the Kopiur Restore of an attempt, for lookups and deletes.
func kopiurStub(restore *restorev1alpha1.Restore) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(kopiurRestoreGVK)
	obj.SetNamespace(restore.Namespace)
	obj.SetName(kopiurName(restore))

	return obj
}

// kopiurRestoreObject returns the desired Kopiur Restore of one attempt. uid is the mover UID when set.
func kopiurRestoreObject(restore *restorev1alpha1.Restore, uid *int64) *unstructured.Unstructured {
	backup := restore.Spec.Source.BackupRef

	spec := map[string]any{
		"source": map[string]any{
			"snapshotRef": map[string]any{keyName: backup.Name, keyNamespace: backupNamespace(restore)},
		},
		"target": map[string]any{"populator": map[string]any{}},
		"policy": map[string]any{"onMissingSnapshot": "Fail"},
	}

	if uid != nil {
		spec["mover"] = map[string]any{
			"securityContext":    map[string]any{"runAsUser": *uid, "runAsGroup": *uid},
			"podSecurityContext": map[string]any{"fsGroup": *uid},
		}
	}

	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": kopiurRestoreGVK.GroupVersion().String(),
		fieldKind:    kopiurRestoreGVK.Kind,
		"metadata": map[string]any{
			keyName:      kopiurName(restore),
			keyNamespace: restore.Namespace,
			"labels":     map[string]any{restoreLabel: restore.Name},
		},
		"spec": spec,
	}}
}

// kopiurPhase reads status.phase of a Kopiur Restore, or "" when it is not set yet.
func kopiurPhase(obj *unstructured.Unstructured) string {
	phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")

	return phase
}

// MoverUID is the UID that the mover writes the restored volume as.
// The check annotation wins. Then the runAsUser of the first main container applies.
// Nil means unset.
func MoverUID(bp *restorev1alpha1.RestoreCheck) (*int64, error) {
	if raw := bp.Annotations[moverUIDAnnotation]; raw != "" {
		var mapping map[string]int64

		err := json.Unmarshal([]byte(raw), &mapping)
		if err != nil {
			return nil, fmt.Errorf("parse %s annotation: %w", moverUIDAnnotation, err)
		}

		if uid, ok := mapping["data"]; ok {
			return &uid, nil
		}
	}

	for i := range bp.Spec.Containers {
		if sc := bp.Spec.Containers[i].SecurityContext; sc != nil && sc.RunAsUser != nil {
			uid := *sc.RunAsUser

			return &uid, nil
		}
	}

	return nil, nil
}

// restoredPVC is the PVC populated by Kopiur. Its dataSourceRef points at the Kopiur Restore.
func restoredPVC(restore *restorev1alpha1.Restore, size resource.Quantity) *corev1.PersistentVolumeClaim {
	apiGroup := kopiurGroup

	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      restoredClaimName(restore),
			Namespace: restore.Namespace,
			Labels:    restoreLabels(restore),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources:        claimResources(size),
			StorageClassName: optionalString(restore.Spec.Target.StorageClassName),
			DataSourceRef: &corev1.TypedObjectReference{
				APIGroup: &apiGroup,
				Kind:     kopiurRestoreGVK.Kind,
				Name:     kopiurName(restore),
			},
		},
	}
}

// scratchPVC is the per-attempt PVC of one check scratch volume.
func scratchPVC(restore *restorev1alpha1.Restore, sv restorev1alpha1.ScratchVolume) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      scratchClaimName(restore, sv.Name),
			Namespace: restore.Namespace,
			Labels:    restoreLabels(restore),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources:        claimResources(sv.Size),
			StorageClassName: optionalString(sv.StorageClassName),
		},
	}
}

func claimResources(size resource.Quantity) corev1.VolumeResourceRequirements {
	return corev1.VolumeResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceStorage: size},
	}
}

// optionalString returns nil for an empty string, so the cluster default applies.
func optionalString(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

// kopiurSnapshotSize reads status.stats.sizeBytes of a Kopiur Snapshot. Nil means missing or zero.
func kopiurSnapshotSize(obj *unstructured.Unstructured) *resource.Quantity {
	bytes, _, _ := unstructured.NestedInt64(obj.Object, "status", "stats", "sizeBytes")
	if bytes <= 0 {
		return nil
	}

	return resource.NewQuantity(bytes, resource.BinarySI)
}
