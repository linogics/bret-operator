package controller

import (
	"archive/tar"
	"compress/gzip"
	"context"
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
	veleroNamespace   = "velero"
	veleroPVCPrefix   = "resources/persistentvolumeclaims/namespaces/"
	veleroMaxPVCBytes = 1 << 20
)

var (
	veleroRestoreGVK   = schema.GroupVersionKind{Group: "velero.io", Version: "v1", Kind: "Restore"}
	downloadRequestGVK = schema.GroupVersionKind{Group: "velero.io", Version: "v1", Kind: "DownloadRequest"}
	blueprintGVK       = schema.GroupVersionKind{Group: "bret.linogics.io", Version: "v1alpha1", Kind: "RestoreBlueprint"}

	// Variables so tests can shorten them.
	veleroDownloadWait = 30 * time.Second
	veleroPollEvery    = 500 * time.Millisecond
	veleroHTTPTimeout  = 60 * time.Second
)

// veleroName names the Velero objects of an attempt. They live in the velero namespace, so the Restore namespace is part of the name.
func veleroName(restore *restorev1alpha1.Restore) string {
	return restore.Namespace + "-" + restore.Name
}

func veleroStub(restore *restorev1alpha1.Restore) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(veleroRestoreGVK)
	obj.SetNamespace(veleroNamespace)
	obj.SetName(veleroName(restore))

	return obj
}

// veleroRestoreObject restores only the PVCs and PVs of the backup into the scratch namespace.
// No owner reference: the object lives in another namespace than the Restore.
func veleroRestoreObject(restore *restorev1alpha1.Restore) *unstructured.Unstructured {
	obj := veleroStub(restore)
	obj.SetLabels(restoreLabels(restore))
	obj.Object["spec"] = map[string]any{
		"backupName":        restore.Spec.Source.BackupRef.Name,
		"includedResources": []any{"persistentvolumeclaims", "persistentvolumes"},
		"namespaceMapping":  map[string]any{restore.Namespace: "bret-scratch-" + restore.Name},
		"restorePVs":        true,
	}

	return obj
}

func veleroScratchNamespace(restore *restorev1alpha1.Restore) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "bret-scratch-" + restore.Name}}
}

// reconcileVelero creates the Velero Restore, then runs the check pod in the scratch namespace once Velero completes.
func (r *RestoreReconciler) reconcileVelero(
	ctx context.Context,
	restore *restorev1alpha1.Restore,
	bp *restorev1alpha1.RestoreBlueprint,
) (ctrl.Result, error) {
	cur := veleroStub(restore)
	phase := ""

	err := r.getObject(ctx, cur)

	switch {
	case apierrors.IsNotFound(err):
		cur = veleroRestoreObject(restore)

		if err := r.Create(ctx, cur); err != nil {
			return ctrl.Result{}, err
		}

		r.Recorder.Event(restore, corev1.EventTypeNormal, reasonRestoreStarted, "started Velero restore "+cur.GetName())
	case err != nil:
		return ctrl.Result{}, err
	default:
		phase, _, _ = unstructured.NestedString(cur.Object, "status", "phase")
	}

	restore.Status.ProviderRef = &restorev1alpha1.ProviderRef{
		Kind: veleroRestoreGVK.Kind, Name: cur.GetName(), Namespace: cur.GetNamespace(),
	}
	setRunning(restore)

	switch phase {
	case "Completed":
		return r.checkVelero(ctx, restore, bp)
	case "Failed", "PartiallyFailed", "FailedValidation":
		r.finish(restore, restorev1alpha1.RestorePhaseFailed, reasonRestoreFailed, "Velero restore "+phase)

		return ctrl.Result{}, nil
	default:
		return ctrl.Result{RequeueAfter: restoreRequeueAfter}, nil
	}
}

// checkVelero runs the check pod next to the restored PVC in the scratch namespace. It requeues
// while Velero has not created the namespace or the PVC yet.
func (r *RestoreReconciler) checkVelero(
	ctx context.Context,
	restore *restorev1alpha1.Restore,
	bp *restorev1alpha1.RestoreBlueprint,
) (ctrl.Result, error) {
	ns := veleroScratchNamespace(restore)

	if err := r.getObject(ctx, ns); err != nil {
		return ctrl.Result{RequeueAfter: restoreRequeueAfter}, client.IgnoreNotFound(err)
	}

	pvcs := &corev1.PersistentVolumeClaimList{}
	if err := r.List(ctx, pvcs, client.InNamespace(ns.Name)); err != nil {
		return ctrl.Result{}, err
	}

	claim, err := r.restoredVeleroClaim(ctx, restore, pvcs.Items)
	if err != nil || claim == "" {
		return ctrl.Result{RequeueAfter: restoreRequeueAfter}, err
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

// restoredVeleroClaim is the PVC Velero restored. With several, the blueprint sourcePVC picks one.
// Scratch claims of the operator carry the restore label and are skipped. "" means not there yet.
func (r *RestoreReconciler) restoredVeleroClaim(
	ctx context.Context,
	restore *restorev1alpha1.Restore,
	pvcs []corev1.PersistentVolumeClaim,
) (string, error) {
	var names []string

	for i := range pvcs {
		if _, ours := pvcs[i].Labels[restoreLabel]; !ours {
			names = append(names, pvcs[i].Name)
		}
	}

	if len(names) == 1 {
		return names[0], nil
	}

	want := r.blueprintSourcePVC(ctx, restore)
	for _, n := range names {
		if n == want {
			return n, nil
		}
	}

	return "", nil
}

// veleroSourceSize reads the PVC size from the backup contents. Failures to read the contents give nil,
// so the caller falls back to target.size. A DownloadRequest that is never answered is an error.
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
		name = r.blueprintSourcePVC(ctx, restore)
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
		"kind":       downloadRequestGVK.Kind,
		"metadata":   map[string]any{keyName: veleroName(restore) + "-size", keyNamespace: veleroNamespace},
		"spec": map[string]any{"target": map[string]any{
			"kind": "BackupContents", keyName: restore.Spec.Source.BackupRef.Name,
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

// blueprintSourcePVC is restoredVolumes[0].sourcePVC of the Restore's blueprint, or "".
// It is read unstructured, so it does not depend on the typed field.
func (r *RestoreReconciler) blueprintSourcePVC(ctx context.Context, restore *restorev1alpha1.Restore) string {
	if restore.Spec.BlueprintRef.Name == "" {
		return ""
	}

	bp := &unstructured.Unstructured{}
	bp.SetGroupVersionKind(blueprintGVK)
	bp.SetNamespace(restore.Namespace)
	bp.SetName(restore.Spec.BlueprintRef.Name)

	if err := r.getObject(ctx, bp); err != nil {
		return ""
	}

	vols, _, _ := unstructured.NestedSlice(bp.Object, "spec", "restoredVolumes")
	if len(vols) == 0 {
		return ""
	}

	first, _ := vols[0].(map[string]any)
	name, _ := first["sourcePVC"].(string)

	return name
}

func fetchPVCSizes(ctx context.Context, url, namespace string) (map[string]resource.Quantity, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	// DisableCompression keeps the body as the gzip archive it is.
	c := &http.Client{Timeout: veleroHTTPTimeout, Transport: &http.Transport{DisableCompression: true}}

	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download backup contents: %s", resp.Status)
	}

	return veleroPVCSizes(resp.Body, namespace)
}

// veleroPVCSizes returns the requested storage of every PVC of namespace in a backup contents tar.gz, by PVC name.
func veleroPVCSizes(contents io.Reader, namespace string) (map[string]resource.Quantity, error) {
	gz, err := gzip.NewReader(contents)
	if err != nil {
		return nil, err
	}
	defer gz.Close()

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
