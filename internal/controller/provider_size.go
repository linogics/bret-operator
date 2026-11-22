package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

// +kubebuilder:rbac:groups=volsync.backube,resources=replicationsources,verbs=get;list;watch

var replicationSourceGVK = schema.GroupVersionKind{Group: "volsync.backube", Version: apiVersionAlpha1, Kind: "ReplicationSource"}

// sourceSize returns the size of the backed-up source, or nil when the provider cannot tell.
func (r *RestoreReconciler) sourceSize(
	ctx context.Context,
	restore *restorev1alpha1.Restore,
	src restorev1alpha1.RestoreSource,
) (*resource.Quantity, error) {
	backup := src.BackupRef
	namespace := restore.Namespace

	switch restore.Spec.Provider {
	case restorev1alpha1.ProviderKopiur:
		snap := &unstructured.Unstructured{}
		snap.SetGroupVersionKind(snapshotGVK)
		snap.SetNamespace(namespace)
		snap.SetName(backup.Name)

		if err := r.getObject(ctx, snap); err != nil {
			return nil, client.IgnoreNotFound(err)
		}

		return kopiurSnapshotSize(snap), nil
	case restorev1alpha1.ProviderVolSync:
		rs := &unstructured.Unstructured{}
		rs.SetGroupVersionKind(replicationSourceGVK)
		rs.SetNamespace(namespace)
		rs.SetName(backup.Name)

		if err := r.getObject(ctx, rs); err != nil {
			return nil, client.IgnoreNotFound(err)
		}

		pvcName, _, _ := unstructured.NestedString(rs.Object, "spec", "sourcePVC")
		if pvcName == "" {
			return nil, nil
		}

		pvc := &corev1.PersistentVolumeClaim{}
		pvc.Namespace, pvc.Name = namespace, pvcName

		if err := r.getObject(ctx, pvc); err != nil {
			return nil, client.IgnoreNotFound(err)
		}

		size, ok := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
		if !ok {
			return nil, nil
		}

		return &size, nil
	default:
		return nil, nil
	}
}
