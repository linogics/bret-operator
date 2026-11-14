package controller

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

// +kubebuilder:rbac:groups=velero.io,resources=downloadrequests,verbs=get;create;delete
// +kubebuilder:rbac:groups=velero.io,resources=restores,verbs=get;list;watch;create;delete

const (
	defaultVeleroNamespace = "velero"
	podSecurityPrefix      = "pod-security.kubernetes.io/"
	veleroPVCPrefix        = "resources/persistentvolumeclaims/namespaces/"
	veleroMaxPVCBytes      = 1 << 20
)

var (
	veleroRestoreGVK   = schema.GroupVersionKind{Group: "velero.io", Version: "v1", Kind: "Restore"}
	downloadRequestGVK = schema.GroupVersionKind{Group: "velero.io", Version: "v1", Kind: "DownloadRequest"}

	// Variables so tests can shorten them.
	veleroDownloadWait = 30 * time.Second
	veleroPollEvery    = 500 * time.Millisecond
	veleroHTTPTimeout  = 60 * time.Second
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
	obj.Object["spec"] = map[string]any{
		"backupName":         restore.Spec.Sources[0].BackupRef.Name,
		"includedNamespaces": []any{restore.Namespace},
		"includedResources":  []any{"persistentvolumeclaims", "persistentvolumes"},
		"namespaceMapping":   map[string]any{restore.Namespace: veleroScratchName(restore)},
		"restorePVs":         true,
	}

	return obj
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

// checkVelero runs the check pod next to the restored PVC in the scratch namespace. It requeues
// while Velero has not created the namespace or the PVC yet.
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
	if err := r.List(ctx, pvcs, client.InNamespace(ns.Name)); err != nil {
		return ctrl.Result{}, err
	}

	claim := r.restoredVeleroClaim(ctx, restore, pvcs.Items)
	if claim == "" {
		return ctrl.Result{RequeueAfter: r.pollInterval()}, nil
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

	return r.check(ctx, restore, bp, claim, ns.Name, claims)
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

// restoredVeleroClaim is the PVC that Velero restored. With several PVCs, the check sourcePVC picks one.
// Scratch claims of the operator carry the restore label and are skipped. An empty string means not there yet.
func (r *RestoreReconciler) restoredVeleroClaim(
	ctx context.Context,
	restore *restorev1alpha1.Restore,
	pvcs []corev1.PersistentVolumeClaim,
) string {
	var names []string

	for i := range pvcs {
		if _, ours := pvcs[i].Labels[restoreLabel]; !ours {
			names = append(names, pvcs[i].Name)
		}
	}

	if len(names) == 1 {
		return names[0]
	}

	want := r.checkSourcePVC(ctx, restore)
	for _, n := range names {
		if n == want {
			return n
		}
	}

	return ""
}

// veleroSourceSize reads the PVC size from the backup contents. If the contents cannot be read, it returns nil.
// The caller then falls back to target.size. A DownloadRequest that is never answered is an error.
func (r *RestoreReconciler) veleroSourceSize(ctx context.Context, restore *restorev1alpha1.Restore) (*resource.Quantity, error) {
	url, err := r.downloadURL(ctx, restore)
	if err != nil {
		return nil, err
	}

	sizes, err := fetchPVCSizes(ctx, url, restore.Namespace)
	if err != nil || len(sizes) == 0 {
		return nil, nil //nolint:nilerr // unreadable contents are not an error, see above
	}

	name := ""

	if len(sizes) == 1 {
		for n := range sizes {
			name = n
		}
	} else {
		name = r.checkSourcePVC(ctx, restore)
	}

	if size, ok := sizes[name]; ok {
		return &size, nil
	}

	return nil, nil
}

// downloadURL creates a DownloadRequest for the backup contents, waits for its URL and deletes the request.
func (r *RestoreReconciler) downloadURL(ctx context.Context, restore *restorev1alpha1.Restore) (string, error) {
	req := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": downloadRequestGVK.GroupVersion().String(),
		fieldKind:    downloadRequestGVK.Kind,
		"metadata":   map[string]any{keyName: veleroName(restore) + "-size", keyNamespace: r.veleroNS()},
		"spec": map[string]any{"target": map[string]any{
			fieldKind: "BackupContents", keyName: restore.Spec.Sources[0].BackupRef.Name,
		}},
	}}

	if err := r.Create(ctx, req); client.IgnoreAlreadyExists(err) != nil {
		return "", err
	}

	defer func() { _ = client.IgnoreNotFound(r.Delete(context.WithoutCancel(ctx), req)) }()

	timeout := time.After(veleroDownloadWait)

	for {
		cur := &unstructured.Unstructured{}
		cur.SetGroupVersionKind(downloadRequestGVK)
		cur.SetNamespace(req.GetNamespace())
		cur.SetName(req.GetName())

		if err := r.getObject(ctx, cur); err != nil && !apierrors.IsNotFound(err) {
			return "", err
		}

		if url, _, _ := unstructured.NestedString(cur.Object, "status", "downloadURL"); url != "" {
			return url, nil
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-timeout:
			return "", errors.New("timed out waiting for Velero DownloadRequest URL")
		case <-time.After(veleroPollEvery):
		}
	}
}

// checkSourcePVC returns restoredVolumes[0].sourcePVC of the Restore's check, or an empty string.
func (r *RestoreReconciler) checkSourcePVC(ctx context.Context, restore *restorev1alpha1.Restore) string {
	if restore.Spec.RestoreCheckRef.Name == "" {
		return ""
	}

	bp := &restorev1alpha1.RestoreCheck{}
	key := client.ObjectKey{Namespace: restore.Namespace, Name: restore.Spec.RestoreCheckRef.Name}

	if err := r.Get(ctx, key, bp); err != nil || len(bp.Spec.RestoredVolumes) == 0 {
		return ""
	}

	return bp.Spec.RestoredVolumes[0].SourcePVC
}

func fetchPVCSizes(ctx context.Context, url, namespace string) (_ map[string]resource.Quantity, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	// DisableCompression keeps the body as the gzip archive it is.
	c := &http.Client{Timeout: veleroHTTPTimeout, Transport: &http.Transport{DisableCompression: true, DisableKeepAlives: true, Proxy: http.ProxyFromEnvironment}}

	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download backup contents: %s", resp.Status)
	}

	return veleroPVCSizes(resp.Body, namespace)
}

// veleroPVCSizes returns the requested storage of every PVC of namespace in a backup contents tar.gz, by PVC name.
func veleroPVCSizes(contents io.Reader, namespace string) (_ map[string]resource.Quantity, err error) {
	gz, err := gzip.NewReader(contents)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, gz.Close()) }()

	prefix := veleroPVCPrefix + namespace + "/"
	sizes := map[string]resource.Quantity{}
	tr := tar.NewReader(gz)

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return sizes, nil
		}

		if err != nil {
			return nil, err
		}

		if !strings.HasPrefix(hdr.Name, prefix) || !strings.HasSuffix(hdr.Name, ".json") {
			continue
		}

		var pvc corev1.PersistentVolumeClaim
		if err := json.NewDecoder(io.LimitReader(tr, veleroMaxPVCBytes)).Decode(&pvc); err != nil {
			continue
		}

		if size, ok := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
			sizes[strings.TrimSuffix(strings.TrimPrefix(hdr.Name, prefix), ".json")] = size
		}
	}
}
