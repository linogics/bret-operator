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

// +kubebuilder:rbac:groups=velero.io,resources=restores,verbs=get;list;watch;create;delete

const (
	defaultVeleroNamespace = "velero"
	podSecurityPrefix      = "pod-security.kubernetes.io/"

	reasonRestoredVolumeMissing = "RestoredVolumeMissing"
)

var (
	veleroRestoreGVK = schema.GroupVersionKind{Group: "velero.io", Version: "v1", Kind: "Restore"}
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
	src := restore.Spec.Sources[0]
	obj.Object["spec"] = map[string]any{
		"backupName":         src.BackupRef.Name,
		"includedNamespaces": []any{restore.Namespace},
		"includedResources":  []any{"persistentvolumeclaims", "persistentvolumes"},
		"namespaceMapping":   map[string]any{restore.Namespace: veleroScratchName(restore)},
		"restorePVs":         true,
	}

	// The selectors are mutually exclusive, the API enforces it.
	if src.LabelSelector != nil {
		setSelector(obj, "labelSelector", src.LabelSelector)
	}

	if len(src.OrLabelSelectors) > 0 {
		sels := make([]any, len(src.OrLabelSelectors))
		for i, sel := range src.OrLabelSelectors {
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

// validateVeleroCheck rejects checks that point at objects of the Restore namespace.
// The Velero check pod runs in the scratch namespace.
func validateVeleroCheck(bp *restorev1alpha1.RestoreCheck) error {
	switch {
	case bp.Spec.ServiceAccountName != "":
		return fmt.Errorf("check %s: serviceAccountName is not supported with Velero", bp.Name)
	case len(bp.Spec.ImagePullSecrets) > 0:
		return fmt.Errorf("check %s: imagePullSecrets is not supported with Velero", bp.Name)
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

	for _, containers := range [][]corev1.Container{bp.Spec.InitContainers, bp.Spec.Containers} {
		for i := range containers {
			if usesSecretOrConfigMap(&containers[i]) {
				return fmt.Errorf("check %s: container %q uses a secret or configMap reference, not supported with Velero", bp.Name, containers[i].Name)
			}
		}
	}

	return nil
}

// usesSecretOrConfigMap reports whether a container reads env or envFrom from a Secret or ConfigMap.
func usesSecretOrConfigMap(c *corev1.Container) bool {
	for _, e := range c.Env {
		if e.ValueFrom != nil && (e.ValueFrom.SecretKeyRef != nil || e.ValueFrom.ConfigMapKeyRef != nil) {
			return true
		}
	}

	for _, ef := range c.EnvFrom {
		if ef.SecretRef != nil || ef.ConfigMapRef != nil {
			return true
		}
	}

	return false
}

func veleroScratchName(restore *restorev1alpha1.Restore) string {
	return "bret-scratch-" + restoreHash(restore)
}

func veleroScratchNamespace(restore *restorev1alpha1.Restore) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: veleroScratchName(restore)}}
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
		cur = r.veleroRestoreObject(restore)

		if err := r.Create(ctx, cur); err != nil {
			return ctrl.Result{}, err
		}

		r.Recorder.Event(restore, corev1.EventTypeNormal, reasonRestoreStarted, "started Velero restore "+cur.GetName())
	case err != nil:
		return ctrl.Result{}, err
	default:
		phase, _, _ = unstructured.NestedString(cur.Object, "status", "phase")
	}

	restore.Status.ProviderRefs = []restorev1alpha1.ProviderRef{{
		Kind: veleroRestoreGVK.Kind, Name: cur.GetName(), Namespace: cur.GetNamespace(),
	}}
	setRunning(restore)

	switch phase {
	case "Completed":
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

	pvcs := &corev1.PersistentVolumeClaimList{}
	if err := r.listObjects(ctx, pvcs, client.InNamespace(ns.Name)); err != nil {
		return ctrl.Result{}, err
	}

	restored, missing := restoredVeleroClaims(pvcs.Items, bp)
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
func (r *RestoreReconciler) copyPodSecurity(ctx context.Context, restore *restorev1alpha1.Restore, scratch *corev1.Namespace) error {
	tenant := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: restore.Namespace}}
	if err := r.getObject(ctx, tenant); err != nil {
		return err
	}

	base := scratch.DeepCopy()

	for k, v := range tenant.Labels {
		if !strings.HasPrefix(k, podSecurityPrefix) {
			continue
		}

		if scratch.Labels == nil {
			scratch.Labels = map[string]string{}
		}

		scratch.Labels[k] = v
	}

	return r.Patch(ctx, scratch, client.MergeFrom(base))
}

// restoredVeleroClaims maps each check sourcePVC to the restored PVC of the same name.
// Scratch claims of the operator carry the restore label and are skipped. missing lists the sourcePVCs
// that were not restored.
func restoredVeleroClaims(pvcs []corev1.PersistentVolumeClaim, bp *restorev1alpha1.RestoreCheck) (claims map[string]string, missing []string) {
	have := make(map[string]bool, len(pvcs))

	for i := range pvcs {
		if _, ours := pvcs[i].Labels[restoreLabel]; !ours {
			have[pvcs[i].Name] = true
		}
	}

	claims = make(map[string]string, len(bp.Spec.RestoredVolumes))

	for _, rv := range bp.Spec.RestoredVolumes {
		if have[rv.SourcePVC] {
			claims[rv.SourcePVC] = rv.SourcePVC
		} else {
			missing = append(missing, rv.SourcePVC)
		}
	}

	return claims, missing
}
