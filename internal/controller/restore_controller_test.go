package controller

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

var _ = Describe("Restore Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: testNamespace, // TODO(user):Modify as needed
		}
		restore := &restorev1alpha1.Restore{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind Restore")

			err := k8sClient.Get(ctx, typeNamespacedName, restore)
			if err != nil && errors.IsNotFound(err) {
				resource := &restorev1alpha1.Restore{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: testNamespace,
					},
					Spec: restorev1alpha1.RestoreSpec{
						Provider: restorev1alpha1.ProviderVelero,
						Source:   restorev1alpha1.RestoreSource{BackupRef: restorev1alpha1.BackupRef{Name: "backup"}},
						Target:   restorev1alpha1.RestoreTarget{Size: quantityPtr("1Gi")},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &restorev1alpha1.Restore{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance Restore")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")

			controllerReconciler := &RestoreReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(10),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})
})

// fakeRestoreClient returns a fake client with the Restore status subresource, and a reconciler on top of it.
func fakeRestoreClient(t *testing.T, objs ...client.Object) (client.Client, *RestoreReconciler) {
	t.Helper()

	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}

	if err := restorev1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}

	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(objs...).
		WithStatusSubresource(&restorev1alpha1.Restore{}, &corev1.Pod{}).
		Build()

	return c, &RestoreReconciler{Client: c, Scheme: s, Recorder: record.NewFakeRecorder(100)}
}

func newTestRestore() *restorev1alpha1.Restore {
	return &restorev1alpha1.Restore{
		ObjectMeta: metav1.ObjectMeta{Name: "r1", Namespace: testNamespace},
		Spec: restorev1alpha1.RestoreSpec{
			Provider:      restorev1alpha1.ProviderKopiur,
			Source:        restorev1alpha1.RestoreSource{BackupRef: restorev1alpha1.BackupRef{Name: "b1"}},
			Target:        restorev1alpha1.RestoreTarget{Size: quantityPtr("1Gi")},
			BlueprintRef:  restorev1alpha1.BlueprintRef{Name: testApp},
			RestoreWindow: metav1.Duration{Duration: 45 * time.Minute},
		},
	}
}

func newTestBlueprint() *restorev1alpha1.RestoreBlueprint {
	return &restorev1alpha1.RestoreBlueprint{
		ObjectMeta: metav1.ObjectMeta{Name: testApp, Namespace: testNamespace},
		Spec: restorev1alpha1.RestoreBlueprintSpec{
			RestoreMount: testMountPath,
			Containers:   []corev1.Container{{Name: testCheckName, Image: "postgres:17"}},
		},
	}
}

func reconcileRestore(t *testing.T, r *RestoreReconciler, key types.NamespacedName) {
	t.Helper()

	if _, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: key}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
}

func getRestore(t *testing.T, c client.Client, key types.NamespacedName) *restorev1alpha1.Restore {
	t.Helper()

	restore := &restorev1alpha1.Restore{}
	if err := c.Get(context.Background(), key, restore); err != nil {
		t.Fatalf("get restore: %v", err)
	}

	return restore
}

func TestRestorePhasesToSucceeded(t *testing.T) {
	ctx := context.Background()
	restore := newTestRestore()
	c, r := fakeRestoreClient(t, restore, newTestBlueprint())
	key := client.ObjectKeyFromObject(restore)

	reconcileRestore(t, r, key)

	if got := getRestore(t, c, key).Status.Phase; got != restorev1alpha1.RestorePhasePending {
		t.Fatalf("after start: phase %q, want Pending", got)
	}

	reconcileRestore(t, r, key)

	if got := getRestore(t, c, key).Status.Phase; got != restorev1alpha1.RestorePhaseRunning {
		t.Fatalf("after Kopiur created: phase %q, want Running", got)
	}

	kopiur := kopiurStub(restore)
	if err := c.Get(ctx, client.ObjectKeyFromObject(kopiur), kopiur); err != nil {
		t.Fatalf("Kopiur restore not created: %v", err)
	}

	if err := unstructured.SetNestedField(kopiur.Object, kopiurPhaseCompleted, "status", "phase"); err != nil {
		t.Fatal(err)
	}

	if err := c.Update(ctx, kopiur); err != nil {
		t.Fatal(err)
	}

	reconcileRestore(t, r, key)

	podKey := types.NamespacedName{Name: checkPodName(restore), Namespace: restore.Namespace}

	pod := &corev1.Pod{}
	if err := c.Get(ctx, podKey, pod); err != nil {
		t.Fatalf("check pod not created: %v", err)
	}

	if got := getRestore(t, c, key).Status.Phase; got != restorev1alpha1.RestorePhaseRunning {
		t.Fatalf("while check runs: phase %q, want Running", got)
	}

	pod.Status.Phase = corev1.PodSucceeded
	if err := c.Status().Update(ctx, pod); err != nil {
		t.Fatal(err)
	}

	reconcileRestore(t, r, key)

	if got := getRestore(t, c, key).Status.Phase; got != restorev1alpha1.RestorePhaseSucceeded {
		t.Fatalf("after check pod succeeded: phase %q, want Succeeded", got)
	}

	// Cleanup deletes the check pod, the Kopiur restore and the restored PVC.
	if err := c.Get(ctx, podKey, &corev1.Pod{}); !errors.IsNotFound(err) {
		t.Errorf("check pod still present after success: %v", err)
	}

	if err := c.Get(ctx, client.ObjectKeyFromObject(kopiur), kopiurStub(restore)); !errors.IsNotFound(err) {
		t.Errorf("Kopiur restore still present after success: %v", err)
	}

	pvc := &corev1.PersistentVolumeClaim{}
	if err := c.Get(ctx, types.NamespacedName{Name: restoredClaimName(restore), Namespace: restore.Namespace}, pvc); !errors.IsNotFound(err) {
		t.Errorf("restored PVC still present after success: %v", err)
	}
}

func TestRestoreWindowCancels(t *testing.T) {
	ctx := context.Background()
	restore := newTestRestore()
	c, r := fakeRestoreClient(t, restore, newTestBlueprint())
	key := client.ObjectKeyFromObject(restore)

	started := metav1.NewTime(time.Now().Add(-2 * time.Hour))
	deadline := metav1.NewTime(started.Add(45 * time.Minute))

	stored := getRestore(t, c, key)
	stored.Status = restorev1alpha1.RestoreStatus{
		Phase:     restorev1alpha1.RestorePhaseRunning,
		Attempt:   1,
		StartTime: &started,
		Deadline:  &deadline,
	}

	if err := c.Status().Update(ctx, stored); err != nil {
		t.Fatal(err)
	}

	reconcileRestore(t, r, key)

	got := getRestore(t, c, key)
	if got.Status.Phase != restorev1alpha1.RestorePhaseCanceled {
		t.Fatalf("past deadline: phase %q, want Canceled", got.Status.Phase)
	}

	if reason := readyReason(got); reason != string(restorev1alpha1.RestorePhaseCanceled) {
		t.Errorf("Ready reason %q, want Canceled", reason)
	}

	if err := c.Get(ctx, client.ObjectKeyFromObject(kopiurStub(restore)), kopiurStub(restore)); !errors.IsNotFound(err) {
		t.Errorf("Kopiur restore created after the window expired: %v", err)
	}
}
