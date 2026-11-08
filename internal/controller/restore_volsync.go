package controller

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

// +kubebuilder:rbac:groups=volsync.backube,resources=replicationdestinations,verbs=get;list;watch;create;delete

// volsyncMoverUID is the mover user of the restic restore when the blueprint sets none. The default mover fails with a setgid error on the scratch volume.
const volsyncMoverUID = int64(65534)

var errRepositoryMissing = errors.New(reasonRepositoryMissing)

var replicationDestinationGVK = schema.GroupVersionKind{Group: "volsync.backube", Version: apiVersionAlpha1, Kind: "ReplicationDestination"}

func volsyncName(restore *restorev1alpha1.Restore) string {
	return restore.Name + "-volsync"
}

func volsyncStub(restore *restorev1alpha1.Restore) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(replicationDestinationGVK)
	obj.SetNamespace(restore.Namespace)
	obj.SetName(volsyncName(restore))

	return obj
}

// volsyncDestinationObject restores the restic repository straight into the scratch PVC.
func volsyncDestinationObject(restore *restorev1alpha1.Restore, repository string, size resource.Quantity, uid *int64) *unstructured.Unstructured {
	mover := volsyncMoverUID
	if uid != nil {
		mover = *uid
	}

	obj := volsyncStub(restore)
	obj.SetLabels(restoreLabels(restore))
	obj.Object["spec"] = map[string]any{
		"trigger": map[string]any{"manual": restore.Name},
		"restic": map[string]any{
			"repository":     repository,
			"copyMethod":     "Direct",
			"destinationPVC": restoredClaimName(restore),
			"capacity":       size.String(),
			"moverSecurityContext": map[string]any{
				"runAsUser": mover, "runAsGroup": mover, "fsGroup": mover,
			},
		},
	}

	return obj
}

// volsyncRepository is the restic repository Secret name of the backup's ReplicationSource, read in the backup namespace only.
// The Secret itself must exist in the Restore namespace. "" means the source is not there.
func (r *RestoreReconciler) volsyncRepository(ctx context.Context, restore *restorev1alpha1.Restore) (string, error) {
	src := &unstructured.Unstructured{}
	src.SetGroupVersionKind(replicationSourceGVK)
	src.SetNamespace(backupNamespace(restore))
	src.SetName(restore.Spec.Source.BackupRef.Name)

	if err := r.getObject(ctx, src); err != nil {
		if apierrors.IsNotFound(err) {
			return "", nil
		}

		return "", err
	}

	repo, _, _ := unstructured.NestedString(src.Object, "spec", "restic", "repository")
	if repo == "" {
		return "", fmt.Errorf("%w: ReplicationSource %s/%s has no spec.restic.repository", errRepositoryMissing, src.GetNamespace(), src.GetName())
	}

	return repo, nil
}

// reconcileVolSync creates the ReplicationDestination that fills the scratch PVC, then runs the check
// once VolSync reports status.latestImage.
func (r *RestoreReconciler) reconcileVolSync(
	ctx context.Context,
	restore *restorev1alpha1.Restore,
	bp *restorev1alpha1.RestoreBlueprint,
	size resource.Quantity,
	uid *int64,
) (ctrl.Result, error) {
	rd := volsyncStub(restore)

	err := r.getObject(ctx, rd)

	switch {
	case apierrors.IsNotFound(err):
		repo, err := r.volsyncRepository(ctx, restore)
		if errors.Is(err, errRepositoryMissing) {
			r.block(restore, reasonRepositoryMissing, err.Error())

			return ctrl.Result{RequeueAfter: restoreRequeueAfter}, nil
		}

		if err != nil || repo == "" {
			return ctrl.Result{RequeueAfter: restoreRequeueAfter}, err
		}

		if _, err := r.ensure(ctx, restore, volsyncScratchPVC(restore, size)); err != nil {
			return ctrl.Result{}, err
		}

		rd = volsyncDestinationObject(restore, repo, size, uid)

		if _, err := r.ensure(ctx, restore, rd); err != nil {
			return ctrl.Result{}, err
		}

		r.Recorder.Event(restore, corev1.EventTypeNormal, reasonRestoreStarted, "started VolSync restore "+rd.GetName())
	case err != nil:
		return ctrl.Result{}, err
	}

	restore.Status.ProviderRef = &restorev1alpha1.ProviderRef{
		Kind: replicationDestinationGVK.Kind, Name: rd.GetName(), Namespace: rd.GetNamespace(),
	}
	setRunning(restore)

	if kind, _, _ := unstructured.NestedString(rd.Object, "status", "latestImage", "kind"); kind != "PersistentVolumeClaim" {
		return ctrl.Result{RequeueAfter: restoreRequeueAfter}, nil
	}

	claims, err := r.ensureScratchClaims(ctx, restore, bp)
	if err != nil {
		return ctrl.Result{}, err
	}

	return r.check(ctx, restore, bp, restoredClaimName(restore), restore.Namespace, claims)
}

// volsyncScratchPVC is the plain PVC VolSync writes into. It has no data source.
func volsyncScratchPVC(restore *restorev1alpha1.Restore, size resource.Quantity) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: restoredClaimName(restore), Namespace: restore.Namespace, Labels: restoreLabels(restore)},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources:        claimResources(size),
			StorageClassName: optionalString(restore.Spec.Target.StorageClassName),
		},
	}
}
