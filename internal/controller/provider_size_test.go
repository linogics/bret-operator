package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

func sizeTestRestore(provider restorev1alpha1.Provider) *restorev1alpha1.Restore {
	return &restorev1alpha1.Restore{
		ObjectMeta: metav1.ObjectMeta{Name: "r", Namespace: "ns"},
		Spec: restorev1alpha1.RestoreSpec{
			Provider: provider,
			Source:   restorev1alpha1.RestoreSource{BackupRef: restorev1alpha1.BackupRef{Name: "backup"}},
		},
	}
}

func unstructuredObj(group, kind, name string, fields map[string]any) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{Object: map[string]any{}}
	obj.SetAPIVersion(group + "/v1alpha1")
	obj.SetKind(kind)
	obj.SetNamespace("ns")
	obj.SetName(name)

	for k, v := range fields {
		obj.Object[k] = v
	}

	return obj
}

func TestSourceSize(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "ns"},
		Spec: corev1.PersistentVolumeClaimSpec{Resources: corev1.VolumeResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("5Gi")},
		}},
	}
	volsyncSource := unstructuredObj("volsync.backube", "ReplicationSource", "backup",
		map[string]any{"spec": map[string]any{"sourcePVC": "data"}})
	snapshot := func(stats map[string]any) *unstructured.Unstructured {
		return unstructuredObj(kopiurGroup, "Snapshot", "backup", map[string]any{"status": map[string]any{"stats": stats}})
	}

	tests := []struct {
		name     string
		provider restorev1alpha1.Provider
		objs     []client.Object
		want     string
	}{
		{"kopiur size", restorev1alpha1.ProviderKopiur, []client.Object{snapshot(map[string]any{"sizeBytes": int64(10485760)})}, "10Mi"},
		{"kopiur zero", restorev1alpha1.ProviderKopiur, []client.Object{snapshot(map[string]any{"sizeBytes": int64(0)})}, ""},
		{"kopiur missing snapshot", restorev1alpha1.ProviderKopiur, nil, ""},
		{"volsync pvc", restorev1alpha1.ProviderVolSync, []client.Object{volsyncSource, pvc}, "5Gi"},
		{"volsync pvc missing", restorev1alpha1.ProviderVolSync, []client.Object{volsyncSource}, ""},
		{"volsync source missing", restorev1alpha1.ProviderVolSync, nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &RestoreReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.objs...).Build()}

			got, err := r.sourceSize(context.Background(), sizeTestRestore(tt.provider))
			if err != nil {
				t.Fatal(err)
			}

			switch {
			case tt.want == "" && got != nil:
				t.Fatalf("want nil, got %s", got)
			case tt.want != "" && (got == nil || got.Cmp(resource.MustParse(tt.want)) != 0):
				t.Fatalf("want %s, got %v", tt.want, got)
			}
		})
	}
}
