package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

// +kubebuilder:rbac:groups=kopiur.home-operations.com,resources=snapshotpolicies,verbs=get

var (
	errSourceMismatch = errors.New(reasonSourceMismatch)
	snapshotPolicyGVK = schema.GroupVersionKind{Group: kopiurGroup, Version: apiVersionAlpha1, Kind: "SnapshotPolicy"}
)

// kopiurRestoreGVK is the Kopiur Restore kind. It is used as unstructured. Kopiur is not a Go dependency.
var kopiurRestoreGVK = schema.GroupVersionKind{Group: kopiurGroup, Version: apiVersionAlpha1, Kind: "Restore"}

// backupNamespace is the namespace of the backup object of a source. It is backupRef.namespace, or else the Restore namespace.
func backupNamespace(restore *restorev1alpha1.Restore, src restorev1alpha1.RestoreSource) string {
	if ns := src.BackupRef.Namespace; ns != "" {
		return ns
	}

	return restore.Namespace
}

// restoreLabels labels every object that an attempt creates. Cleanup uses them to find the objects.
func restoreLabels(restore *restorev1alpha1.Restore) map[string]string {
	return map[string]string{restoreLabel: restore.Name}
}

// kopiurRestoreObject returns the desired Kopiur Restore of one source. uid is the mover UID when set.
func kopiurRestoreObject(restore *restorev1alpha1.Restore, src restorev1alpha1.RestoreSource, uid *int64) *unstructured.Unstructured {
	backup := src.BackupRef

	spec := map[string]any{
		"source": map[string]any{
			"snapshotRef": map[string]any{keyName: backup.Name, keyNamespace: backupNamespace(restore, src)},
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
			keyName:      kopiurName(restore, src.SourcePVC),
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

// MoverUID is the UID that the mover writes the restored volume of sourcePVC as.
// The check annotation, keyed by sourcePVC, wins. Then the runAsUser of the first main container applies.
// Nil means unset.
func MoverUID(bp *restorev1alpha1.RestoreCheck, sourcePVC string) (*int64, error) {
	if raw := bp.Annotations[moverUIDAnnotation]; raw != "" {
		var mapping map[string]int64

		err := json.Unmarshal([]byte(raw), &mapping)
		if err != nil {
			return nil, fmt.Errorf("parse %s annotation: %w", moverUIDAnnotation, err)
		}

		if uid, ok := mapping[sourcePVC]; ok {
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
func restoredPVC(restore *restorev1alpha1.Restore, src restorev1alpha1.RestoreSource, size resource.Quantity) *corev1.PersistentVolumeClaim {
	apiGroup := kopiurGroup

	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      restoredClaimName(restore, src.SourcePVC),
			Namespace: restore.Namespace,
			Labels:    restoreLabels(restore),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources:        claimResources(size),
			StorageClassName: optionalString(src.StorageClassName),
			DataSourceRef: &corev1.TypedObjectReference{
				APIGroup: &apiGroup,
				Kind:     kopiurRestoreGVK.Kind,
				Name:     kopiurName(restore, src.SourcePVC),
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

// kopiurSnapshotPVC is the name of the PVC that a Kopiur Snapshot backs up. Empty means unknown.
// The Snapshot mirrors it in status.resolved.sources as "namespace/name". A fanned-out Snapshot names it in the spec.
// A single-source SnapshotPolicy is the last resort. policy may be nil.
func kopiurSnapshotPVC(snap, policy *unstructured.Unstructured) string {
	sources, _, _ := unstructured.NestedSlice(snap.Object, "status", "resolved", "sources")
	if len(sources) > 0 {
		if first, ok := sources[0].(map[string]any); ok {
			if pvc, _ := first["pvc"].(string); pvc != "" {
				return pvc[strings.LastIndex(pvc, "/")+1:]
			}
		}
	}

	if name, _, _ := unstructured.NestedString(snap.Object, "spec", "source", "target", "pvc", "name"); name != "" {
		return name
	}

	if policy == nil {
		return ""
	}

	sources, _, _ = unstructured.NestedSlice(policy.Object, "spec", "sources")
	if len(sources) > 0 {
		if first, ok := sources[0].(map[string]any); ok {
			name, _, _ := unstructured.NestedString(first, "pvc", "name")

			return name
		}
	}

	return ""
}

// verifyKopiurSources reads the Snapshot of every source and fails when it backs up another PVC than the sourcePVC.
// A Snapshot or policy that cannot be read leaves the PVC unknown, which is no mismatch.
func (r *RestoreReconciler) verifyKopiurSources(ctx context.Context, restore *restorev1alpha1.Restore) error {
	for _, src := range restore.Spec.Sources {
		snap := &unstructured.Unstructured{}
		snap.SetGroupVersionKind(snapshotGVK)
		snap.SetNamespace(backupNamespace(restore, src))
		snap.SetName(src.BackupRef.Name)

		if err := r.getObject(ctx, snap); err != nil {
			if apierrors.IsNotFound(err) {
				continue // The Kopiur Restore reports a missing Snapshot.
			}

			return err
		}

		var policy *unstructured.Unstructured

		if name, _, _ := unstructured.NestedString(snap.Object, "spec", "policyRef", keyName); name != "" {
			policy = &unstructured.Unstructured{}
			policy.SetGroupVersionKind(snapshotPolicyGVK)
			policy.SetNamespace(snap.GetNamespace())
			policy.SetName(name)

			if err := r.getObject(ctx, policy); err != nil {
				if !apierrors.IsNotFound(err) && !apierrors.IsForbidden(err) {
					return err
				}

				policy = nil
			}
		}

		if got := kopiurSnapshotPVC(snap, policy); got != "" && got != src.SourcePVC {
			return fmt.Errorf("%w: snapshot %s/%s backs up PVC %q, but the source is %q", errSourceMismatch, snap.GetNamespace(), snap.GetName(), got, src.SourcePVC)
		}
	}

	return nil
}
