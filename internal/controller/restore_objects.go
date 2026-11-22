package controller

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

// errObjectTerminating means an object of the attempt is still being deleted. The attempt waits for it.
var errObjectTerminating = errors.New("object is being deleted")

// retainedLabel marks objects kept after a failed attempt when keepOnFailure is set.
const retainedLabel = "bret.linogics.io/retained"

// ensure creates obj, owned by restore, unless it exists. obj is filled with the stored object
// and the result reports whether this call created it.
func (r *RestoreReconciler) ensure(ctx context.Context, restore *restorev1alpha1.Restore, obj client.Object) (bool, error) {
	err := r.getObject(ctx, obj)
	if err == nil {
		if obj.GetLabels()[restoreLabel] != restore.Name {
			return false, fmt.Errorf("%T %s/%s exists and does not belong to restore %s", obj, obj.GetNamespace(), obj.GetName(), restore.Name)
		}

		// A dying object of an earlier Restore with the same name must not count for this one.
		if !obj.GetDeletionTimestamp().IsZero() {
			return false, fmt.Errorf("%w: %T %s/%s", errObjectTerminating, obj, obj.GetNamespace(), obj.GetName())
		}

		return false, nil
	}

	if !apierrors.IsNotFound(err) {
		return false, err
	}

	// Objects in another namespace cannot have an owner. Their cleanup finds them by name and label.
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
// It is idempotent. A failed run is retried on the next reconcile.
func (r *RestoreReconciler) cleanup(ctx context.Context, restore *restorev1alpha1.Restore) error {
	// A deleted Restore keeps nothing.
	keep := restore.Spec.KeepOnFailure && restore.Status.Phase != restorev1alpha1.RestorePhaseSucceeded && restore.DeletionTimestamp.IsZero()

	pvcs := &corev1.PersistentVolumeClaimList{}

	err := r.listObjects(ctx, pvcs, client.InNamespace(restore.Namespace), client.MatchingLabels(restoreLabels(restore)))
	if err != nil {
		return err
	}

	objs := []client.Object{
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: checkPodName(restore), Namespace: restore.Namespace}},
	}

	// Objects of a disabled provider are not read. The operator has no rights for them.
	if r.providerEnabled(restorev1alpha1.ProviderKopiur) {
		found, listErr := r.labelled(ctx, restore, kopiurRestoreGVK)
		if listErr != nil {
			return listErr
		}

		objs = append(objs, found...)
	}

	if restore.Spec.Provider == restorev1alpha1.ProviderVelero && r.providerEnabled(restorev1alpha1.ProviderVelero) {
		// Ownership is read before the Velero Restore goes, because it can be the only proof.
		owned, ownErr := r.ownsScratch(ctx, restore)
		if ownErr != nil {
			return ownErr
		}

		if owned {
			objs = append(objs, veleroScratchNamespace(restore))
		}

		objs = append(objs, r.veleroStub(restore))
	}

	if restore.Spec.Provider == restorev1alpha1.ProviderVolSync && r.providerEnabled(restorev1alpha1.ProviderVolSync) {
		found, listErr := r.labelled(ctx, restore, replicationDestinationGVK)
		if listErr != nil {
			return listErr
		}

		objs = append(objs, found...)
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
		return ignoreUncleanable(err)
	}

	// The Velero scratch Namespace may carry no label. cleanup adds it only after ownsScratch.
	if _, ours := obj.GetLabels()[restoreLabel]; !ours && obj.GetNamespace() != "" {
		return nil
	}

	if !keep {
		return ignoreUncleanable(r.Delete(ctx, obj))
	}

	base := obj.DeepCopyObject().(client.Object)

	labels := obj.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}

	labels[retainedLabel] = "true"
	obj.SetLabels(labels)

	return ignoreUncleanable(r.Patch(ctx, obj, client.MergeFrom(base)))
}

// ignoreUncleanable drops the errors of objects that cannot exist or cannot be reached.
// Such errors cover a missing object, a CRD that is not installed, or no RBAC for it.
// Returning them would keep the finalizer forever.
func ignoreUncleanable(err error) error {
	if apierrors.IsNotFound(err) || apierrors.IsForbidden(err) || meta.IsNoMatchError(err) {
		return nil
	}

	return err
}

// getObject reads obj through the API reader when one is set. Provider objects use it.
// No informer is started for them.
func (r *RestoreReconciler) getObject(ctx context.Context, obj client.Object) error {
	if r.APIReader != nil {
		return r.APIReader.Get(ctx, client.ObjectKeyFromObject(obj), obj)
	}

	return r.Get(ctx, client.ObjectKeyFromObject(obj), obj)
}

// listObjects lists through the API reader when one is set. Provider objects use it.
// No informer is started for them.
func (r *RestoreReconciler) listObjects(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if r.APIReader != nil {
		return r.APIReader.List(ctx, list, opts...)
	}

	return r.List(ctx, list, opts...)
}

// labelled returns the objects of a kind in the Restore namespace that carry the label of the Restore.
// A kind that is not installed or not readable has none.
func (r *RestoreReconciler) labelled(ctx context.Context, restore *restorev1alpha1.Restore, gvk schema.GroupVersionKind) ([]client.Object, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk.GroupVersion().WithKind(gvk.Kind + "List"))

	err := r.listObjects(ctx, list, client.InNamespace(restore.Namespace), client.MatchingLabels(restoreLabels(restore)))
	if err != nil {
		return nil, ignoreUncleanable(err)
	}

	objs := make([]client.Object, 0, len(list.Items))
	for i := range list.Items {
		objs = append(objs, &list.Items[i])
	}

	return objs, nil
}
