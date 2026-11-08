package controller

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

func TestVeleroRestoreSpec(t *testing.T) {
	restore := sizeTestRestore("velero")
	obj := (&RestoreReconciler{}).veleroRestoreObject(restore)

	if obj.GetNamespace() != "velero" || obj.GetAPIVersion() != "velero.io/v1" || obj.GetKind() != "Restore" {
		t.Fatalf("unexpected identity %s %s/%s", obj.GetAPIVersion(), obj.GetNamespace(), obj.GetKind())
	}

	want := map[string]any{
		"backupName":         testBackupName,
		"includedNamespaces": []any{"ns"},
		"includedResources":  []any{"persistentvolumeclaims", "persistentvolumes"},
		"namespaceMapping":   map[string]any{"ns": veleroScratchName(restore)},
		"restorePVs":         true,
	}

	if got, _, _ := unstructured.NestedMap(obj.Object, "spec"); !reflect.DeepEqual(got, want) {
		t.Fatalf("spec = %v, want %v", got, want)
	}
}

// pvcTarGz builds a Velero backup contents archive with one PVC file per "namespace/name: size" entry.
func pvcTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for path, size := range files {
		body := `{"spec":{"resources":{"requests":{"storage":"` + size + `"}}}}`
		if size == "" {
			body = "{}"
		}

		hdr := &tar.Header{Name: "resources/persistentvolumeclaims/namespaces/" + path + ".json", Mode: 0o644, Size: int64(len(body))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}

		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

func TestVeleroPVCSizes(t *testing.T) {
	data := pvcTarGz(t, map[string]string{testPVCKeyA: testSize5Gi, testPVCKeyB: testSize10Gi, "other/c": testSize1Gi, "ns/empty": ""})

	got, err := veleroPVCSizes(bytes.NewReader(data), "ns")
	if err != nil {
		t.Fatal(err)
	}

	a, b := got["a"], got["b"]
	if len(got) != 2 || a.Cmp(resource.MustParse(testSize5Gi)) != 0 || b.Cmp(resource.MustParse(testSize10Gi)) != 0 {
		t.Fatalf("unexpected sizes %v", got)
	}

	if _, err := veleroPVCSizes(bytes.NewReader([]byte("not gzip")), "ns"); err == nil {
		t.Fatal("want error for non gzip input")
	}
}

func TestVeleroSourceSize(t *testing.T) {
	check := unstructuredObj("bret.linogics.io", "RestoreCheck", "bp", map[string]any{
		"spec": map[string]any{"restoredVolumes": []any{map[string]any{"name": testDataName, "sourcePVC": "b"}}},
	})

	tests := []struct {
		name   string
		files  map[string]string
		status int
		bp     bool
		want   string
	}{
		{"single pvc", map[string]string{testPVCKeyA: testSize5Gi}, 200, false, testSize5Gi},
		{"check picks", map[string]string{testPVCKeyA: testSize5Gi, testPVCKeyB: testSize10Gi}, 200, true, testSize10Gi},
		{"ambiguous without check", map[string]string{testPVCKeyA: testSize5Gi, testPVCKeyB: testSize10Gi}, 200, false, ""},
		{"no pvc in namespace", map[string]string{"other/a": testSize5Gi}, 200, false, ""},
		{"download fails", nil, 500, false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := pvcTarGz(t, tt.files)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tt.status != 200 {
					w.WriteHeader(tt.status)

					return
				}

				_, _ = w.Write(body)
			}))

			defer srv.Close()

			scheme := runtime.NewScheme()
			if err := clientgoscheme.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}

			objs := []client.Object{}
			if tt.bp {
				objs = append(objs, check.DeepCopy())
			}

			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
			r := &RestoreReconciler{Client: c}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			go answerDownloadRequest(ctx, c, srv.URL)

			restore := sizeTestRestore("velero")
			restore.Spec.RestoreCheckRef.Name = "bp"

			got, err := r.veleroSourceSize(ctx, restore)
			if err != nil {
				t.Fatal(err)
			}

			switch {
			case tt.want == "" && got != nil:
				t.Fatalf("want nil, got %s", got)
			case tt.want != "" && (got == nil || got.Cmp(resource.MustParse(tt.want)) != 0):
				t.Fatalf("want %s, got %v", tt.want, got)
			}

			left := &unstructured.UnstructuredList{}
			left.SetGroupVersionKind(downloadRequestGVK)

			if err := c.List(context.Background(), left); err != nil || len(left.Items) != 0 {
				t.Fatalf("DownloadRequest not deleted: %v %d", err, len(left.Items))
			}
		})
	}
}

// answerDownloadRequest plays Velero: it sets status.downloadURL on any DownloadRequest it finds.
func answerDownloadRequest(ctx context.Context, c client.Client, url string) {
	for ctx.Err() == nil {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(downloadRequestGVK)

		if err := c.List(ctx, list); err == nil && len(list.Items) > 0 {
			obj := &list.Items[0]
			_ = unstructured.SetNestedField(obj.Object, url, "status", "downloadURL")
			_ = c.Update(ctx, obj)

			return
		}

		time.Sleep(10 * time.Millisecond)
	}
}

func TestVeleroCheckRunsInScratchNamespace(t *testing.T) {
	for _, tt := range []struct {
		name      string
		podPhase  corev1.PodPhase
		keep      bool
		wantPhase restorev1alpha1.RestorePhase
		wantNS    bool
	}{
		{"success deletes scratch namespace", corev1.PodSucceeded, false, restorev1alpha1.RestorePhaseSucceeded, false},
		{"failure with keepOnFailure retains it", corev1.PodFailed, true, restorev1alpha1.RestorePhaseFailed, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(pvcTarGz(t, map[string]string{testNamespace + "/data": testSize1Gi}))
			}))
			defer srv.Close()

			restore := newTestRestore()
			restore.Spec.Provider = restorev1alpha1.ProviderVelero
			restore.Spec.KeepOnFailure = tt.keep

			c, r := fakeRestoreClient(t, restore, newTestCheck(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}})
			key := client.ObjectKeyFromObject(restore)

			go func() {
				for ctx.Err() == nil {
					answerDownloadRequest(ctx, c, srv.URL)
				}
			}()

			reconcileRestore(t, r, key) // Pending
			reconcileRestore(t, r, key) // Velero Restore created

			vr := r.veleroStub(restore)
			if err := c.Get(ctx, client.ObjectKeyFromObject(vr), vr); err != nil {
				t.Fatalf("Velero restore not created: %v", err)
			}

			if err := unstructured.SetNestedField(vr.Object, "Completed", "status", "phase"); err != nil {
				t.Fatal(err)
			}

			if err := c.Update(ctx, vr); err != nil {
				t.Fatal(err)
			}

			scratch := veleroScratchNamespace(restore)
			podKey := types.NamespacedName{Name: checkPodName(restore), Namespace: scratch.Name}

			reconcileRestore(t, r, key) // namespace absent: requeue, no error

			if err := c.Get(ctx, podKey, &corev1.Pod{}); !errors.IsNotFound(err) {
				t.Fatalf("pod created before the scratch namespace exists: %v", err)
			}

			pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: testDataName, Namespace: scratch.Name}}
			if err := c.Create(ctx, scratch); err != nil {
				t.Fatal(err)
			}

			if err := c.Create(ctx, pvc); err != nil {
				t.Fatal(err)
			}

			reconcileRestore(t, r, key)

			pod := &corev1.Pod{}
			if err := c.Get(ctx, podKey, pod); err != nil {
				t.Fatalf("check pod not in scratch namespace: %v", err)
			}

			if pod.Spec.Volumes[0].PersistentVolumeClaim.ClaimName != testDataName {
				t.Fatalf("pod mounts %v, want restored PVC data", pod.Spec.Volumes[0])
			}

			pod.Status.Phase = tt.podPhase
			if err := c.Status().Update(ctx, pod); err != nil {
				t.Fatal(err)
			}

			reconcileRestore(t, r, key)

			if got := getRestore(t, c, key).Status.Phase; got != tt.wantPhase {
				t.Fatalf("phase %q, want %q", got, tt.wantPhase)
			}

			err := c.Get(ctx, client.ObjectKeyFromObject(scratch), scratch)
			if tt.wantNS != (err == nil) {
				t.Fatalf("scratch namespace present=%v, want %v (%v)", err == nil, tt.wantNS, err)
			}

			if tt.wantNS && scratch.Labels[retainedLabel] != "true" {
				t.Fatalf("kept namespace not labelled retained: %v", scratch.Labels)
			}

			// Velero restore is cleaned the same way, and a second cleanup is a no-op.
			if err := r.cleanup(ctx, getRestore(t, c, key)); err != nil {
				t.Fatalf("second cleanup: %v", err)
			}

			err = c.Get(ctx, client.ObjectKeyFromObject(vr), r.veleroStub(restore))
			if tt.wantNS == errors.IsNotFound(err) {
				t.Fatalf("velero restore presence wrong, keep=%v err=%v", tt.keep, err)
			}
		})
	}
}

func TestVeleroRejectsRestoreNamespaceReferences(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*restorev1alpha1.RestoreCheck)
		field  string
	}{
		{"service account", func(bp *restorev1alpha1.RestoreCheck) { bp.Spec.ServiceAccountName = "sa" }, "serviceAccountName"},
		{"pull secrets", func(bp *restorev1alpha1.RestoreCheck) {
			bp.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: "s"}}
		}, "imagePullSecrets"},
		{"configmap volume", func(bp *restorev1alpha1.RestoreCheck) {
			bp.Spec.Volumes = []corev1.Volume{{Name: "v", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{}}}}
		}, "volumes"},
		{"secret volume", func(bp *restorev1alpha1.RestoreCheck) {
			bp.Spec.Volumes = []corev1.Volume{{Name: "v", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{}}}}
		}, "volumes"},
		{"clean", func(*restorev1alpha1.RestoreCheck) {}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(pvcTarGz(t, map[string]string{testNamespace + "/data": testSize1Gi}))
			}))
			defer srv.Close()

			restore := newTestRestore()
			restore.Spec.Provider = restorev1alpha1.ProviderVelero

			bp := newTestCheck()
			tt.mutate(bp)

			c, r := fakeRestoreClient(t, restore, bp)
			key := client.ObjectKeyFromObject(restore)

			go func() {
				for ctx.Err() == nil {
					answerDownloadRequest(ctx, c, srv.URL)
				}
			}()

			reconcileRestore(t, r, key) // Pending
			reconcileRestore(t, r, key)

			got := getRestore(t, c, key)
			err := c.Get(ctx, client.ObjectKeyFromObject(r.veleroStub(restore)), r.veleroStub(restore))

			if tt.field == "" {
				if err != nil || got.Status.Phase != restorev1alpha1.RestorePhaseRunning {
					t.Fatalf("clean check: phase %q, velero restore err %v", got.Status.Phase, err)
				}

				return
			}

			if got.Status.Phase != restorev1alpha1.RestorePhaseFailed || readyReason(got) != reasonProviderUnsupported ||
				!strings.Contains(got.Status.Message, tt.field) {
				t.Fatalf("phase %q reason %q message %q", got.Status.Phase, readyReason(got), got.Status.Message)
			}

			if !errors.IsNotFound(err) {
				t.Fatalf("velero restore created for rejected check: %v", err)
			}
		})
	}
}

func TestVeleroRejectsHostPath(t *testing.T) {
	bp := newTestCheck()
	bp.Spec.Volumes = []corev1.Volume{{Name: "v", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/"}}}}

	if err := validateVeleroCheck(bp); err == nil || !strings.Contains(err.Error(), "hostPath") {
		t.Fatalf("err = %v, want a hostPath rejection", err)
	}
}

func TestVeleroRejectsSecretAndConfigMapRefs(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*restorev1alpha1.RestoreCheck)
		wantErr bool
	}{
		{"secretKeyRef env", func(bp *restorev1alpha1.RestoreCheck) {
			bp.Spec.Containers[0].Env = []corev1.EnvVar{{Name: "P", ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "s"}, Key: "k"},
			}}}
		}, true},
		{"configMapRef envFrom", func(bp *restorev1alpha1.RestoreCheck) {
			bp.Spec.Containers[0].EnvFrom = []corev1.EnvFromSource{{ConfigMapRef: &corev1.ConfigMapEnvSource{}}}
		}, true},
		{"plain env", func(bp *restorev1alpha1.RestoreCheck) {
			bp.Spec.Containers[0].Env = []corev1.EnvVar{{Name: "P", Value: "v"}}
		}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bp := newTestCheck()
			tt.mutate(bp)

			err := validateVeleroCheck(bp)
			if tt.wantErr && (err == nil || !strings.Contains(err.Error(), "secret or configMap reference")) {
				t.Fatalf("err = %v, want a secret or configMap rejection", err)
			}

			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected err = %v", err)
			}
		})
	}
}
