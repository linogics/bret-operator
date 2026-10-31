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
	moverUIDAnnotation   = "restore.bret.linogics.io/mover-uid"
	keyName              = "name"
	keyNamespace         = "namespace"
)

// kopiurRestoreGVK is the Kopiur Restore kind. It is used as unstructured, so Kopiur is not a Go dependency.
var kopiurRestoreGVK = schema.GroupVersionKind{Group: kopiurGroup, Version: "v1alpha1", Kind: "Restore"}

// kopiurName is the name of the Kopiur Restore of one attempt.
func kopiurName(restore *restorev1alpha1.Restore) string {
	return restore.Name + "-kopiur"
}

// restoredClaimName is the PVC that receives the restored data.
func restoredClaimName(restore *restorev1alpha1.Restore) string {
	return restore.Name + "-scratch"
}

// scratchClaimName is the PVC of one blueprint scratch volume.
func scratchClaimName(restore *restorev1alpha1.Restore, volume string) string {
	return restore.Name + "-scratch-" + dnsLabel(volume)
}

// restoreLabels labels every object an attempt creates, so cleanup can find them.
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

// kopiurRestoreObject returns the desired Kopiur Restore of one attempt. uid, when set, is the mover UID.
func kopiurRestoreObject(restore *restorev1alpha1.Restore, uid *int64) *unstructured.Unstructured {
	backup := restore.Spec.Source.BackupRef

	backupNamespace := backup.Namespace
	if backupNamespace == "" {
		backupNamespace = restore.Namespace
	}

	spec := map[string]any{
		"source": map[string]any{
			"snapshotRef": map[string]any{keyName: backup.Name, keyNamespace: backupNamespace},
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
		"kind":       kopiurRestoreGVK.Kind,
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

// moverUID is the UID the mover writes the restored volume as. The blueprint annotation wins,
// then the first main container's runAsUser, as in hack/blueprint-test/render.py. Nil means unset.
func moverUID(bp *restorev1alpha1.RestoreBlueprint) (*int64, error) {
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
func restoredPVC(restore *restorev1alpha1.Restore) *corev1.PersistentVolumeClaim {
	apiGroup := kopiurGroup

	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      restoredClaimName(restore),
			Namespace: restore.Namespace,
			Labels:    restoreLabels(restore),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources:        claimResources(restore.Spec.Target.Size),
			StorageClassName: optionalString(restore.Spec.Target.StorageClassName),
			DataSourceRef: &corev1.TypedObjectReference{
				APIGroup: &apiGroup,
				Kind:     kopiurRestoreGVK.Kind,
				Name:     kopiurName(restore),
			},
		},
	}
}

// scratchPVC is the per-attempt PVC of one blueprint scratch volume.
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
