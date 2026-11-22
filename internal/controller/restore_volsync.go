package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

// +kubebuilder:rbac:groups=volsync.backube,resources=replicationdestinations,verbs=get;list;watch;create;delete;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get

// volsyncMoverUID is the mover user of the restic restore when the check sets none.
// The default mover fails with a setgid error on the scratch volume.
const volsyncMoverUID = int64(65534)

var errRepositoryMissing = errors.New(reasonRepositoryMissing)

var replicationDestinationGVK = schema.GroupVersionKind{Group: "volsync.backube", Version: apiVersionAlpha1, Kind: "ReplicationDestination"}

func volsyncStub(restore *restorev1alpha1.Restore, src restorev1alpha1.RestoreSource) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(replicationDestinationGVK)
	obj.SetNamespace(restore.Namespace)
	obj.SetName(volsyncName(restore, src.SourcePVC))

	return obj
}

// volsyncDestinationObject restores the restic repository straight into the scratch PVC of one source.
func volsyncDestinationObject(restore *restorev1alpha1.Restore, src restorev1alpha1.RestoreSource, repository string, size resource.Quantity, uid *int64) *unstructured.Unstructured {
	mover := volsyncMoverUID
	if uid != nil {
		mover = *uid
	}

	restic := map[string]any{
		"repository":     repository,
		"copyMethod":     "Direct",
		"destinationPVC": restoredClaimName(restore, src.SourcePVC),
		"capacity":       size.String(),
		"moverSecurityContext": map[string]any{
			"runAsUser": mover, "runAsGroup": mover, "fsGroup": mover,
		},
	}

	// Restic restores the newest snapshot at or before this time, so a later sync does not change the run.
	if src.RestoreAsOf != nil {
		restic["restoreAsOf"] = src.RestoreAsOf.UTC().Format(time.RFC3339)
	}

	obj := volsyncStub(restore, src)
	obj.SetLabels(restoreLabels(restore))
	obj.Object["spec"] = map[string]any{
		"trigger": map[string]any{"manual": restore.Name},
		"restic":  restic,
	}

	return obj
}

// volsyncRepository is the restic repository Secret name of the backup's ReplicationSource, and the PVC it backs up.
// Both are read in the Restore namespace only.
// An empty repo means the source is not there. An empty sourcePVC means the source does not name one.
func (r *RestoreReconciler) volsyncRepository(ctx context.Context, restore *restorev1alpha1.Restore, src restorev1alpha1.RestoreSource) (repo, sourcePVC string, err error) {
	rs := &unstructured.Unstructured{}
	rs.SetGroupVersionKind(replicationSourceGVK)
	rs.SetNamespace(restore.Namespace)
	rs.SetName(src.BackupRef.Name)

	if err := r.getObject(ctx, rs); err != nil {
		if apierrors.IsNotFound(err) {
			return "", "", nil
		}

		return "", "", err
	}

	sourcePVC, _, _ = unstructured.NestedString(rs.Object, "spec", "sourcePVC")

	repo, _, _ = unstructured.NestedString(rs.Object, "spec", "restic", "repository")
	if repo == "" {
		return "", sourcePVC, fmt.Errorf("%w: ReplicationSource %s/%s has no spec.restic.repository", errRepositoryMissing, rs.GetNamespace(), rs.GetName())
	}

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: repo, Namespace: restore.Namespace}}
	if err := r.getObject(ctx, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return "", sourcePVC, fmt.Errorf("%w: secret %q not found in namespace %q", errRepositoryMissing, repo, restore.Namespace)
		}

		return "", "", err
	}

	return repo, sourcePVC, nil
}

// reconcileVolSync creates one scratch PVC and one ReplicationDestination per source.
// Nothing is created until every missing destination has a readable repository and the right source PVC.
// It runs the check once every destination reports status.latestImage.
func (r *RestoreReconciler) reconcileVolSync(
	ctx context.Context,
	restore *restorev1alpha1.Restore,
	bp *restorev1alpha1.RestoreCheck,
	sizes map[string]resource.Quantity,
	uids map[string]*int64,
) (ctrl.Result, error) {
	repos := make(map[string]string, len(restore.Spec.Sources))

	for _, src := range restore.Spec.Sources {
		err := r.getObject(ctx, volsyncStub(restore, src))
		if err == nil {
			continue
		}

		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}

		repo, sourcePVC, err := r.volsyncRepository(ctx, restore, src)
		if errors.Is(err, errRepositoryMissing) {
			restore.Status.Phase = restorev1alpha1.RestorePhaseFailed
			r.block(restore, reasonRepositoryMissing, err.Error())

			return ctrl.Result{}, nil
		}

		if err != nil || repo == "" {
			return ctrl.Result{RequeueAfter: r.pollInterval()}, err
		}

		if sourcePVC != "" && sourcePVC != src.SourcePVC {
			restore.Status.Phase = restorev1alpha1.RestorePhaseFailed
			r.block(restore, reasonSourceMismatch, fmt.Sprintf("ReplicationSource %s/%s backs up PVC %q, but the source is %q", restore.Namespace, src.BackupRef.Name, sourcePVC, src.SourcePVC))

			return ctrl.Result{}, nil
		}

		repos[src.SourcePVC] = repo
	}

	var (
		refs     []restorev1alpha1.ProviderRef
		restored = make(map[string]string, len(restore.Spec.Sources))
		ready    int
	)

	for _, src := range restore.Spec.Sources {
		rd := volsyncDestinationObject(restore, src, repos[src.SourcePVC], sizes[src.SourcePVC], uids[src.SourcePVC])

		if repo, ok := repos[src.SourcePVC]; ok && repo != "" {
			if _, err := r.ensure(ctx, restore, volsyncScratchPVC(restore, src, sizes[src.SourcePVC])); err != nil {
				return ctrl.Result{}, err
			}

			if _, err := r.ensure(ctx, restore, rd); err != nil {
				return ctrl.Result{}, err
			}

			r.Recorder.Event(restore, corev1.EventTypeNormal, reasonRestoreStarted, "started VolSync restore "+rd.GetName())
		} else if err := r.getObject(ctx, rd); err != nil {
			return ctrl.Result{}, err
		}

		refs = append(refs, restorev1alpha1.ProviderRef{Kind: replicationDestinationGVK.Kind, Name: rd.GetName(), Namespace: rd.GetNamespace()})
		restored[src.SourcePVC] = restoredClaimName(restore, src.SourcePVC)

		// The restic mover exits 0 and VolSync sets latestImage when no snapshot fits restoreAsOf
		// (volsync mover-restic/entry.sh, do_restore). The volume is then empty.
		logs, _, _ := unstructured.NestedString(rd.Object, "status", "latestMoverStatus", "logs")
		if strings.Contains(logs, "No eligible snapshots found") {
			restore.Status.Phase = restorev1alpha1.RestorePhaseFailed
			r.block(restore, reasonBackupNotFound, "VolSync found no snapshot at or before restoreAsOf for "+src.SourcePVC+", nothing was restored")

			return ctrl.Result{}, nil
		}

		if kind, _, _ := unstructured.NestedString(rd.Object, "status", "latestImage", "kind"); kind == "PersistentVolumeClaim" {
			ready++
		}
	}

	restore.Status.ProviderRefs = refs
	setRunning(restore)

	if ready < len(restore.Spec.Sources) {
		return ctrl.Result{RequeueAfter: r.pollInterval()}, nil
	}

	claims, err := r.ensureScratchClaims(ctx, restore, bp)
	if err != nil {
		return ctrl.Result{}, err
	}

	return r.check(ctx, restore, bp, restored, restore.Namespace, claims)
}

// volsyncScratchPVC is the plain PVC VolSync writes into. It has no data source.
func volsyncScratchPVC(restore *restorev1alpha1.Restore, src restorev1alpha1.RestoreSource, size resource.Quantity) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: restoredClaimName(restore, src.SourcePVC), Namespace: restore.Namespace, Labels: restoreLabels(restore)},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources:        claimResources(size),
			StorageClassName: optionalString(src.StorageClassName),
		},
	}
}
