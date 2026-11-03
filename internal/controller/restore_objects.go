package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

// retainedLabel marks objects kept after a failed attempt when keepOnFailure is set.
const retainedLabel = "bret.linogics.io/retained"

// ensure creates obj, owned by restore, unless it exists. obj is filled with the stored object
// and the result reports whether this call created it.
func (r *RestoreReconciler) ensure(ctx context.Context, restore *restorev1alpha1.Restore, obj client.Object) (bool, error) {
	err := r.getObject(ctx, obj)
	if err == nil {
		return false, nil
	}

	if !apierrors.IsNotFound(err) {
		return false, err
	}

	// Objects in another namespace cannot have an owner; their cleanup finds them by name and label.
	if obj.GetNamespace() == restore.Namespace {
		err = controllerutil.SetControllerReference(restore, obj, r.Scheme)
		if err != nil {
			return false, err
		}
	}

	err = r.Create(ctx, obj)
	if err != nil {
		return false, err
	}

	return true, nil
}

// cleanup deletes the objects of an attempt, or labels them as retained when they must be kept.
// It is idempotent, so a failed run is retried on the next reconcile.
func (r *RestoreReconciler) cleanup(ctx context.Context, restore *restorev1alpha1.Restore) error {
	keep := restore.Spec.KeepOnFailure && restore.Status.Phase != restorev1alpha1.RestorePhaseSucceeded

	pvcs := &corev1.PersistentVolumeClaimList{}

	err := r.List(ctx, pvcs, client.InNamespace(restore.Namespace), client.MatchingLabels(restoreLabels(restore)))
	if err != nil {
		return err
	}

	objs := []client.Object{
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: checkPodName(restore), Namespace: restore.Namespace}},
		kopiurStub(restore),
	}

	if restore.Spec.Provider == restorev1alpha1.ProviderVelero {
		objs = append(objs, veleroStub(restore), veleroScratchNamespace(restore))
	}

	for i := range pvcs.Items {
		objs = append(objs, &pvcs.Items[i])
	}

	for _, obj := range objs {
		err = r.cleanupObject(ctx, obj, keep)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *RestoreReconciler) cleanupObject(ctx context.Context, obj client.Object, keep bool) error {
	err := r.getObject(ctx, obj)
	if err != nil {
		return client.IgnoreNotFound(err)
	}

	if !keep {
		return client.IgnoreNotFound(r.Delete(ctx, obj))
	}

	base := obj.DeepCopyObject().(client.Object)

	labels := obj.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}

	labels[retainedLabel] = "true"
	obj.SetLabels(labels)

	return r.Patch(ctx, obj, client.MergeFrom(base))
}

// getObject reads obj through the API reader when one is set. Provider objects use it, so no
// informer is started for them.
func (r *RestoreReconciler) getObject(ctx context.Context, obj client.Object) error {
	if r.APIReader != nil {
		return r.APIReader.Get(ctx, client.ObjectKeyFromObject(obj), obj)
	}

	return r.Get(ctx, client.ObjectKeyFromObject(obj), obj)
}
