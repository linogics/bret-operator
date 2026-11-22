package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

// +kubebuilder:rbac:groups=velero.io,resources=restores,verbs=get;list;watch;create;delete;patch
// +kubebuilder:rbac:groups=velero.io,resources=backups;podvolumebackups,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets;configmaps,verbs=get;create

const (
	defaultVeleroNamespace = "velero"
	podSecurityPrefix      = "pod-security.kubernetes.io/"

	reasonRestoredVolumeMissing = "RestoredVolumeMissing"
	reasonCheckRefMissing       = "CheckRefMissing"
	reasonBackupUnsupported     = "BackupUnsupported"

	veleroGroup = "velero.io"
)

var (
	veleroRestoreGVK = schema.GroupVersionKind{Group: veleroGroup, Version: "v1", Kind: "Restore"}
	veleroBackupGVK  = schema.GroupVersionKind{Group: veleroGroup, Version: "v1", Kind: "Backup"}
	veleroPVBGVK     = schema.GroupVersionKind{Group: veleroGroup, Version: "v1", Kind: "PodVolumeBackupList"}
)

// restoreHash is a short hash of namespace/name. The hash keeps the names of objects outside the Restore
// namespace unique. It also keeps them valid for any Restore name length.
func restoreHash(restore *restorev1alpha1.Restore) string {
	sum := sha256.Sum256([]byte(restore.Namespace + "/" + restore.Name))

	return hex.EncodeToString(sum[:])[:12]
}

// veleroName names the Velero objects of an attempt.
func veleroName(restore *restorev1alpha1.Restore) string {
	return "bret-" + restoreHash(restore)
}

// veleroPhaseCompleted is the phase of a Velero Backup or Restore that finished without errors.
const veleroPhaseCompleted = "Completed"

// veleroNS is the Velero install namespace.
func (r *RestoreReconciler) veleroNS() string {
	if r.VeleroNamespace == "" {
		return defaultVeleroNamespace
	}

	return r.VeleroNamespace
}

func (r *RestoreReconciler) veleroStub(restore *restorev1alpha1.Restore) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(veleroRestoreGVK)
	obj.SetNamespace(r.veleroNS())
	obj.SetName(veleroName(restore))

	return obj
}

// veleroRestoreObject restores only the PVCs and PVs of the backup into the scratch namespace.
// It sets no owner reference. The object lives in another namespace than the Restore.
func (r *RestoreReconciler) veleroRestoreObject(restore *restorev1alpha1.Restore) *unstructured.Unstructured {
	obj := r.veleroStub(restore)
	obj.SetLabels(restoreLabels(restore))
	obj.Object["spec"] = map[string]any{
		"backupName":         restore.Spec.Sources[0].BackupRef.Name,
		"includedNamespaces": []any{restore.Namespace},
		"includedResources":  []any{"persistentvolumeclaims", "persistentvolumes"},
		"namespaceMapping":   map[string]any{restore.Namespace: veleroScratchName(restore)},
		"restorePVs":         true,
	}

	opts := restore.Spec.Velero
	if opts == nil {
		return obj
	}

	// The selectors are mutually exclusive, the API enforces it.
	if opts.LabelSelector != nil {
		setSelector(obj, "labelSelector", opts.LabelSelector)
	}

	if len(opts.OrLabelSelectors) > 0 {
		sels := make([]any, len(opts.OrLabelSelectors))
		for i, sel := range opts.OrLabelSelectors {
			sels[i] = selectorMap(sel)
		}

		_ = unstructured.SetNestedSlice(obj.Object, sels, "spec", "orLabelSelectors")
	}

	return obj
}

// selectorMap converts a label selector to its unstructured form.
func selectorMap(sel *metav1.LabelSelector) map[string]any {
	m, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(sel)

	return m
}

func setSelector(obj *unstructured.Unstructured, field string, sel *metav1.LabelSelector) {
	_ = unstructured.SetNestedMap(obj.Object, selectorMap(sel), "spec", field)
}

// validateVeleroCheck rejects checks that point at objects of the Restore namespace that the operator does not copy.
// The Velero check pod runs in the scratch namespace. checkRefs lists the objects that are copied.
func validateVeleroCheck(bp *restorev1alpha1.RestoreCheck) error {
	if bp.Spec.ServiceAccountName != "" {
		return fmt.Errorf("check %s: serviceAccountName is not supported with Velero", bp.Name)
	}

	for i := range bp.Spec.Volumes {
		v := bp.Spec.Volumes[i]

		if v.ConfigMap != nil || v.Secret != nil || v.Projected != nil || v.PersistentVolumeClaim != nil {
			return fmt.Errorf("check %s: volumes[%d] %q uses a configMap, secret, projected or persistentVolumeClaim volume, not supported with Velero", bp.Name, i, v.Name)
		}

		if v.HostPath != nil {
			return fmt.Errorf("check %s: volumes[%d] %q uses a hostPath, not supported with Velero", bp.Name, i, v.Name)
		}
	}

	return nil
}

// checkRef is a Secret or ConfigMap of the Restore namespace that the check pod reads.
type checkRef struct {
	secret   bool
	name     string
	optional bool
}

func (c checkRef) String() string {
	if c.secret {
		return "secret " + c.name
	}

	return "configMap " + c.name
}

// checkRefs lists the Secrets and ConfigMaps named by imagePullSecrets, env and envFrom of a check.
// A name can appear more than once.
func checkRefs(bp *restorev1alpha1.RestoreCheck) []checkRef {
	refs := make([]checkRef, 0, len(bp.Spec.ImagePullSecrets))

	for _, s := range bp.Spec.ImagePullSecrets {
		refs = append(refs, checkRef{secret: true, name: s.Name})
	}

	for _, containers := range [][]corev1.Container{bp.Spec.InitContainers, bp.Spec.Containers} {
		for i := range containers {
			refs = append(refs, containerRefs(&containers[i])...)
		}
	}

	return refs
}

func containerRefs(c *corev1.Container) []checkRef {
	var refs []checkRef

	for _, e := range c.Env {
		switch {
		case e.ValueFrom == nil:
		case e.ValueFrom.SecretKeyRef != nil:
			refs = append(refs, checkRef{secret: true, name: e.ValueFrom.SecretKeyRef.Name, optional: isTrue(e.ValueFrom.SecretKeyRef.Optional)})
		case e.ValueFrom.ConfigMapKeyRef != nil:
			refs = append(refs, checkRef{name: e.ValueFrom.ConfigMapKeyRef.Name, optional: isTrue(e.ValueFrom.ConfigMapKeyRef.Optional)})
		}
	}

	for _, ef := range c.EnvFrom {
		switch {
		case ef.SecretRef != nil:
			refs = append(refs, checkRef{secret: true, name: ef.SecretRef.Name, optional: isTrue(ef.SecretRef.Optional)})
		case ef.ConfigMapRef != nil:
			refs = append(refs, checkRef{name: ef.ConfigMapRef.Name, optional: isTrue(ef.ConfigMapRef.Optional)})
		}
	}

	return refs
}

func isTrue(b *bool) bool {
	return b != nil && *b
}

// copyCheckRefs copies the Secrets and ConfigMaps of the check from the Restore namespace into the scratch namespace.
// The copies carry the restore label and go with the scratch namespace. missing names the first required object
// that is not in the Restore namespace.
func (r *RestoreReconciler) copyCheckRefs(
	ctx context.Context,
	restore *restorev1alpha1.Restore,
	bp *restorev1alpha1.RestoreCheck,
	ns string,
) (missing string, err error) {
	for _, ref := range checkRefs(bp) {
		// Kubernetes creates this ConfigMap in every namespace.
		if !ref.secret && ref.name == "kube-root-ca.crt" {
			continue
		}

		found, err := r.copyCheckRef(ctx, restore, ref, ns)
		if err != nil {
			return "", err
		}

		if !found && !ref.optional {
			return ref.String(), nil
		}
	}

	return "", nil
}

// copyCheckRef copies one object. found is false when the Restore namespace does not have it.
func (r *RestoreReconciler) copyCheckRef(ctx context.Context, restore *restorev1alpha1.Restore, ref checkRef, ns string) (found bool, err error) {
	src := metav1.ObjectMeta{Name: ref.name, Namespace: restore.Namespace}
	dst := metav1.ObjectMeta{Name: ref.name, Namespace: ns, Labels: restoreLabels(restore)}

	if ref.secret {
		s := &corev1.Secret{ObjectMeta: src}
		if err := r.getObject(ctx, s); err != nil {
			return false, client.IgnoreNotFound(err)
		}

		_, err = r.ensure(ctx, restore, &corev1.Secret{ObjectMeta: dst, Type: s.Type, Data: s.Data})

		return true, err
	}

	cm := &corev1.ConfigMap{ObjectMeta: src}
	if err := r.getObject(ctx, cm); err != nil {
		return false, client.IgnoreNotFound(err)
	}

	_, err = r.ensure(ctx, restore, &corev1.ConfigMap{ObjectMeta: dst, Data: cm.Data, BinaryData: cm.BinaryData})

	return true, err
}

func veleroScratchName(restore *restorev1alpha1.Restore) string {
	return "bret-scratch-" + restoreHash(restore)
}

func veleroScratchNamespace(restore *restorev1alpha1.Restore) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: veleroScratchName(restore)}}
}

// ownsScratch reports whether the scratch namespace belongs to the Restore. It does when it carries the restore
// label, or when the Velero Restore of the Restore still exists. Velero creates the namespace without a label,
// and a Velero restore that failed early leaves it unlabeled.
func (r *RestoreReconciler) ownsScratch(ctx context.Context, restore *restorev1alpha1.Restore) (bool, error) {
	ns := veleroScratchNamespace(restore)

	if err := r.getObject(ctx, ns); err != nil {
		return false, ignoreUncleanable(err)
	}

	if ns.Labels[restoreLabel] == restore.Name {
		return true, nil
	}

	vr := r.veleroStub(restore)

	if err := r.getObject(ctx, vr); err != nil {
		return false, ignoreUncleanable(err)
	}

	return vr.GetLabels()[restoreLabel] == restore.Name, nil
}

// reconcileVelero creates the Velero Restore. It runs the check pod in the scratch namespace once Velero completes.
func (r *RestoreReconciler) reconcileVelero(
	ctx context.Context,
	restore *restorev1alpha1.Restore,
	bp *restorev1alpha1.RestoreCheck,
) (ctrl.Result, error) {
	cur := r.veleroStub(restore)
	phase := ""

	err := r.getObject(ctx, cur)

	switch {
	case apierrors.IsNotFound(err):
		problem, err := r.veleroBackupProblem(ctx, restore)
		if err != nil {
			return ctrl.Result{}, err
		}

		if problem != "" {
			restore.Status.Phase = restorev1alpha1.RestorePhaseFailed
			r.block(restore, reasonBackupUnsupported, problem)

			return ctrl.Result{}, nil
		}

		cur = r.veleroRestoreObject(restore)

		if err := r.Create(ctx, cur); err != nil {
			return ctrl.Result{}, err
		}

		r.Recorder.Event(restore, corev1.EventTypeNormal, reasonRestoreStarted, "started Velero restore "+cur.GetName())
	case err != nil:
		return ctrl.Result{}, err
	case cur.GetLabels()[restoreLabel] != restore.Name:
		return ctrl.Result{}, fmt.Errorf("velero Restore %s/%s exists and does not belong to restore %s", cur.GetNamespace(), cur.GetName(), restore.Name)
	case !cur.GetDeletionTimestamp().IsZero():
		// The Velero Restore of an earlier Restore with this name. Its phase is not this attempt's.
		return ctrl.Result{}, fmt.Errorf("%w: velero Restore %s/%s", errObjectTerminating, cur.GetNamespace(), cur.GetName())
	default:
		phase, _, _ = unstructured.NestedString(cur.Object, "status", "phase")
	}

	restore.Status.ProviderRefs = []restorev1alpha1.ProviderRef{{
		Kind: veleroRestoreGVK.Kind, Name: cur.GetName(), Namespace: cur.GetNamespace(),
	}}
	setRunning(restore)

	switch phase {
	case veleroPhaseCompleted:
		// Velero creates the scratch namespace only while it restores an item. Without items nothing will come.
		if items, _, _ := unstructured.NestedInt64(cur.Object, "status", "progress", "itemsRestored"); items == 0 {
			restore.Status.Phase = restorev1alpha1.RestorePhaseFailed
			r.block(restore, reasonRestoredVolumeMissing, "Velero restore "+cur.GetName()+" completed without restoring any item")

			return ctrl.Result{}, nil
		}

		return r.checkVelero(ctx, restore, bp)
	case "Failed", "PartiallyFailed", "FailedValidation":
		r.finish(restore, restorev1alpha1.RestorePhaseFailed, reasonRestoreFailed, "Velero restore "+phase)

		return ctrl.Result{}, nil
	default:
		return ctrl.Result{RequeueAfter: r.pollInterval()}, nil
	}
}

// checkVelero runs the check pod next to the restored PVCs in the scratch namespace. It requeues
// while Velero has not created the namespace. A restored PVC that is missing fails the attempt.
func (r *RestoreReconciler) checkVelero(
	ctx context.Context,
	restore *restorev1alpha1.Restore,
	bp *restorev1alpha1.RestoreCheck,
) (ctrl.Result, error) {
	ns := veleroScratchNamespace(restore)

	if err := r.getObject(ctx, ns); err != nil {
		return ctrl.Result{RequeueAfter: r.pollInterval()}, client.IgnoreNotFound(err)
	}

	if err := r.copyPodSecurity(ctx, restore, ns); err != nil {
		return ctrl.Result{}, err
	}

	missingRef, err := r.copyCheckRefs(ctx, restore, bp, ns.Name)
	if err != nil {
		return ctrl.Result{}, err
	}

	if missingRef != "" {
		// Without the object the check pod never starts.
		restore.Status.Phase = restorev1alpha1.RestorePhaseFailed
		r.block(restore, reasonCheckRefMissing, "check needs "+missingRef+", not found in namespace "+restore.Namespace)

		return ctrl.Result{}, nil
	}

	pvcs := &corev1.PersistentVolumeClaimList{}
	if err := r.listObjects(ctx, pvcs, client.InNamespace(ns.Name)); err != nil {
		return ctrl.Result{}, err
	}

	restored, missing := restoredVeleroClaims(pvcs.Items, restore.Spec.Sources)
	if len(missing) > 0 {
		// Velero is Completed, so the missing claims will not come.
		restore.Status.Phase = restorev1alpha1.RestorePhaseFailed
		r.block(restore, reasonRestoredVolumeMissing, "Velero did not restore: "+strings.Join(missing, ", "))

		return ctrl.Result{}, nil
	}

	claims := make(map[string]string, len(bp.Spec.ScratchVolumes))

	for _, sv := range bp.Spec.ScratchVolumes {
		pvc := scratchPVC(restore, sv)
		pvc.Namespace = ns.Name

		if _, err := r.ensure(ctx, restore, pvc); err != nil {
			return ctrl.Result{}, err
		}

		claims[sv.Name] = pvc.Name
	}

	return r.check(ctx, restore, bp, restored, ns.Name, claims)
}

// copyPodSecurity puts the pod-security labels of the Restore namespace on the scratch namespace.
// The check pod then runs under the same admission rules as in the tenant namespace.
// It also labels the namespace with the Restore, which marks it as ours for cleanup.
func (r *RestoreReconciler) copyPodSecurity(ctx context.Context, restore *restorev1alpha1.Restore, scratch *corev1.Namespace) error {
	tenant := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: restore.Namespace}}
	if err := r.getObject(ctx, tenant); err != nil {
		return err
	}

	base := scratch.DeepCopy()

	if scratch.Labels == nil {
		scratch.Labels = map[string]string{}
	}

	scratch.Labels[restoreLabel] = restore.Name

	for k, v := range tenant.Labels {
		if !strings.HasPrefix(k, podSecurityPrefix) {
			continue
		}

		scratch.Labels[k] = v
	}

	return r.Patch(ctx, scratch, client.MergeFrom(base))
}

// restoredVeleroClaims maps each source PVC to the restored PVC of the same name.
// Scratch claims of the operator carry the restore label and are skipped. missing lists the sourcePVCs
// that were not restored.
func restoredVeleroClaims(pvcs []corev1.PersistentVolumeClaim, sources []restorev1alpha1.RestoreSource) (claims map[string]string, missing []string) {
	have := make(map[string]bool, len(pvcs))

	for i := range pvcs {
		if _, ours := pvcs[i].Labels[restoreLabel]; !ours {
			have[pvcs[i].Name] = true
		}
	}

	claims = make(map[string]string, len(sources))

	for _, src := range sources {
		if have[src.SourcePVC] {
			claims[src.SourcePVC] = src.SourcePVC
		} else {
			missing = append(missing, src.SourcePVC)
		}
	}

	return claims, missing
}

// veleroBackupProblem explains why the Backup holds no volume data that a PVC-only restore can bring back.
// Velero writes file system backup data only while it restores the pod that mounted the volume
// (velero pkg/restore/restore.go, restorePodVolumeBackups). bret restores no pods, so such a PVC comes back empty.
// A Backup with snapshotVolumes false holds no snapshot either, so its PVCs come back empty too.
func (r *RestoreReconciler) veleroBackupProblem(ctx context.Context, restore *restorev1alpha1.Restore) (string, error) {
	name := restore.Spec.Sources[0].BackupRef.Name

	backup := &unstructured.Unstructured{}
	backup.SetGroupVersionKind(veleroBackupGVK)
	backup.SetNamespace(r.veleroNS())
	backup.SetName(name)

	if err := r.getObject(ctx, backup); err != nil {
		// A missing Backup fails the Velero restore with a clear phase.
		return "", client.IgnoreNotFound(err)
	}

	if snap, found, _ := unstructured.NestedBool(backup.Object, "spec", "snapshotVolumes"); found && !snap {
		return "Velero Backup " + name + " has snapshotVolumes false, so it holds no volume data a PVC restore can bring back", nil
	}

	pvbs := &unstructured.UnstructuredList{}
	pvbs.SetGroupVersionKind(veleroPVBGVK)

	if err := r.listObjects(ctx, pvbs, client.InNamespace(r.veleroNS()), client.MatchingLabels{"velero.io/backup-name": name}); err != nil {
		return "", err
	}

	sources := make(map[string]bool, len(restore.Spec.Sources))
	for _, src := range restore.Spec.Sources {
		sources[src.SourcePVC] = true
	}

	for _, pvb := range pvbs.Items {
		ns, _, _ := unstructured.NestedString(pvb.Object, "spec", "tags", "ns")
		pvc := pvb.GetAnnotations()["velero.io/pvc-name"]

		if ns == restore.Namespace && sources[pvc] {
			return "PVC " + pvc + " is in Velero Backup " + name + " as file system backup, which Velero restores only with its pod", nil
		}
	}

	return "", nil
}
