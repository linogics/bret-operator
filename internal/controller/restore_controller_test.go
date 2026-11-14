package controller

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus/testutil"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
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
			Namespace: testNamespace,
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
						Provider: restorev1alpha1.ProviderVolSync,
						Sources: []restorev1alpha1.RestoreSource{{
							SourcePVC:     testDataName,
							BackupRef:     restorev1alpha1.BackupRef{Name: testBackupName},
							RestoreTarget: restorev1alpha1.RestoreTarget{Size: quantityPtr("1Gi")},
						}},

						RestoreCheckRef: restorev1alpha1.RestoreCheckRef{Name: "bp"},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
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
			Provider: restorev1alpha1.ProviderKopiur,
			Sources: []restorev1alpha1.RestoreSource{{
				SourcePVC:     testDataName,
				BackupRef:     restorev1alpha1.BackupRef{Name: "b1"},
				RestoreTarget: restorev1alpha1.RestoreTarget{Size: quantityPtr("1Gi")},
			}},
			RestoreCheckRef: restorev1alpha1.RestoreCheckRef{Name: testApp},
			RestoreWindow:   metav1.Duration{Duration: 45 * time.Minute},
		},
	}
}

func newTestCheck() *restorev1alpha1.RestoreCheck {
	return &restorev1alpha1.RestoreCheck{
		ObjectMeta: metav1.ObjectMeta{Name: testApp, Namespace: testNamespace},
		Spec: restorev1alpha1.RestoreCheckSpec{
			RestoredVolumes: []restorev1alpha1.RestoredVolume{{SourcePVC: testDataName, MountPath: testMountPath}},
			Containers:      []corev1.Container{{Name: testCheckName, Image: "postgres:17"}},
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
	c, r := fakeRestoreClient(t, restore, newTestCheck())
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
	c, r := fakeRestoreClient(t, restore, newTestCheck())
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

// kopiurSnapshotOfSize is a Kopiur Snapshot named b1 that reports sizeBytes.
func kopiurSnapshotOfSize(sizeBytes int64) *unstructured.Unstructured {
	snap := &unstructured.Unstructured{}
	snap.SetGroupVersionKind(snapshotGVK)
	snap.SetNamespace(testNamespace)
	snap.SetName("b1")
	_ = unstructured.SetNestedField(snap.Object, sizeBytes, "status", "stats", "sizeBytes")

	return snap
}

// reconcileToSizing runs the start step and the step that resolves the size.
func reconcileToSizing(t *testing.T, r *RestoreReconciler, key types.NamespacedName) {
	t.Helper()

	reconcileRestore(t, r, key)
	reconcileRestore(t, r, key)
}

func assertSizeFailure(t *testing.T, c client.Client, restore *restorev1alpha1.Restore, reason string) {
	t.Helper()

	got := getRestore(t, c, client.ObjectKeyFromObject(restore))
	if got.Status.Phase != restorev1alpha1.RestorePhaseFailed {
		t.Fatalf("phase %q, want Failed", got.Status.Phase)
	}

	if r := readyReason(got); r != reason {
		t.Errorf("Ready reason %q, want %s", r, reason)
	}

	pvc := &corev1.PersistentVolumeClaim{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: restoredClaimName(restore), Namespace: restore.Namespace}, pvc); !errors.IsNotFound(err) {
		t.Errorf("PVC created for a failed size: %v", err)
	}

	if err := c.Get(context.Background(), client.ObjectKeyFromObject(kopiurStub(restore)), kopiurStub(restore)); !errors.IsNotFound(err) {
		t.Errorf("provider object created for a failed size: %v", err)
	}
}

func TestRestoreSizeUnknownFails(t *testing.T) {
	restore := newTestRestore()
	restore.Spec.Sources[0].Size = nil
	c, r := fakeRestoreClient(t, restore, newTestCheck())

	reconcileToSizing(t, r, client.ObjectKeyFromObject(restore))
	assertSizeFailure(t, c, restore, restorev1alpha1.ReasonSizeUnknown)
}

func TestRestoreSizeTooSmallFails(t *testing.T) {
	restore := newTestRestore()
	restore.Spec.Sources[0].Size = quantityPtr("1Gi")
	c, r := fakeRestoreClient(t, restore, newTestCheck(), kopiurSnapshotOfSize(2<<30))

	reconcileToSizing(t, r, client.ObjectKeyFromObject(restore))
	assertSizeFailure(t, c, restore, restorev1alpha1.ReasonInsufficientCapacity)
}

func TestRestoreSizeAutoExpand(t *testing.T) {
	restore := newTestRestore()
	restore.Spec.Sources[0].Size = nil
	restore.Spec.Sources[0].AutoExpand = &restorev1alpha1.AutoExpand{Percentage: 150} // 2Gi + 150%
	c, r := fakeRestoreClient(t, restore, newTestCheck(), kopiurSnapshotOfSize(2<<30))

	reconcileToSizing(t, r, client.ObjectKeyFromObject(restore))

	pvc := &corev1.PersistentVolumeClaim{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: restoredClaimName(restore), Namespace: restore.Namespace}, pvc); err != nil {
		t.Fatal(err)
	}

	got := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	if want := resource.MustParse("5Gi"); got.Cmp(want) != 0 {
		t.Errorf("PVC size %s, want 5Gi", got.String())
	}
}

func foreignCheckPod(restore *restorev1alpha1.Restore) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: checkPodName(restore), Namespace: restore.Namespace},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "x", Image: "x"}}},
		Status:     corev1.PodStatus{Phase: corev1.PodSucceeded},
	}
}

func TestEnsureRejectsForeignObject(t *testing.T) {
	ctx := context.Background()
	restore := newTestRestore()
	c, r := fakeRestoreClient(t, restore, newTestCheck(), foreignCheckPod(restore))
	key := client.ObjectKeyFromObject(restore)

	reconcileRestore(t, r, key)
	reconcileRestore(t, r, key)

	kopiur := kopiurStub(restore)
	if err := c.Get(ctx, client.ObjectKeyFromObject(kopiur), kopiur); err != nil {
		t.Fatal(err)
	}

	if err := unstructured.SetNestedField(kopiur.Object, kopiurPhaseCompleted, "status", "phase"); err != nil {
		t.Fatal(err)
	}

	if err := c.Update(ctx, kopiur); err != nil {
		t.Fatal(err)
	}

	if _, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key}); err == nil {
		t.Fatal("reconcile accepted a check pod that the operator did not create")
	}

	if got := getRestore(t, c, key).Status.Phase; got == restorev1alpha1.RestorePhaseSucceeded {
		t.Errorf("phase = %q, foreign pod must not decide the result", got)
	}
}

func TestCleanupSkipsForeignObject(t *testing.T) {
	ctx := context.Background()
	restore := newTestRestore()
	c, r := fakeRestoreClient(t, restore, newTestCheck(), foreignCheckPod(restore))
	key := client.ObjectKeyFromObject(restore)

	reconcileRestore(t, r, key)

	if err := c.Delete(ctx, getRestore(t, c, key)); err != nil {
		t.Fatal(err)
	}

	reconcileRestore(t, r, key)

	if err := c.Get(ctx, key, &restorev1alpha1.Restore{}); !errors.IsNotFound(err) {
		t.Fatalf("restore not released: %v", err)
	}

	pod := foreignCheckPod(restore)
	if err := c.Get(ctx, client.ObjectKeyFromObject(pod), pod); err != nil {
		t.Errorf("foreign pod was deleted by cleanup: %v", err)
	}
}

func TestRestoreCheckUnsupportedRequeues(t *testing.T) {
	ctx := context.Background()
	restore := newTestRestore()
	check := newTestCheck()
	check.Spec.Volumes = []corev1.Volume{{Name: "v", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/"}}}}

	c, r := fakeRestoreClient(t, restore, check)
	key := client.ObjectKeyFromObject(restore)

	reconcileRestore(t, r, key)

	res, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
	if err != nil {
		t.Fatal(err)
	}

	if res.RequeueAfter != r.pollInterval() {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, r.pollInterval())
	}

	if got := readyReason(getRestore(t, c, key)); got != reasonCheckUnsupported {
		t.Fatalf("reason = %q, want %s", got, reasonCheckUnsupported)
	}

	stored := &restorev1alpha1.RestoreCheck{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(check), stored); err != nil {
		t.Fatal(err)
	}

	stored.Annotations = map[string]string{allowHostPathAnnotation: testTrue}
	if err := c.Update(ctx, stored); err != nil {
		t.Fatal(err)
	}

	reconcileRestore(t, r, key)

	if got := getRestore(t, c, key).Status.Phase; got != restorev1alpha1.RestorePhaseRunning {
		t.Errorf("phase = %q, want Running after the annotation", got)
	}
}

func TestRestoreWindowCancelsWithoutCheck(t *testing.T) {
	ctx := context.Background()
	restore := newTestRestore()
	c, r := fakeRestoreClient(t, restore)
	key := client.ObjectKeyFromObject(restore)

	started := metav1.NewTime(time.Now().Add(-2 * time.Hour))
	deadline := metav1.NewTime(started.Add(45 * time.Minute))

	stored := getRestore(t, c, key)
	stored.Status = restorev1alpha1.RestoreStatus{
		Phase:     restorev1alpha1.RestorePhasePending,
		Attempt:   1,
		StartTime: &started,
		Deadline:  &deadline,
	}

	if err := c.Status().Update(ctx, stored); err != nil {
		t.Fatal(err)
	}

	reconcileRestore(t, r, key)

	if got := getRestore(t, c, key).Status.Phase; got != restorev1alpha1.RestorePhaseCanceled {
		t.Errorf("phase = %q, want Canceled", got)
	}
}

func TestRestoreRunCountedOnce(t *testing.T) {
	ctx := context.Background()
	restore := newTestRestore()
	c, r := fakeRestoreClient(t, restore, newTestCheck())
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

	fail := true
	r.Client = interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
		SubResourceUpdate: func(ctx context.Context, cl client.Client, sub string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
			if _, ok := obj.(*restorev1alpha1.Restore); ok && fail {
				fail = false

				return stderrors.New("conflict (simulated)")
			}

			return cl.SubResource(sub).Update(ctx, obj, opts...)
		},
	})

	counter := restoreRuns.WithLabelValues(string(restorev1alpha1.ProviderKopiur), "canceled")
	before := testutil.ToFloat64(counter)

	if _, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key}); err == nil {
		t.Fatal("first reconcile: want status update error")
	}

	reconcileRestore(t, r, key)
	reconcileRestore(t, r, key)

	if got := testutil.ToFloat64(counter) - before; got != 1 {
		t.Errorf("runs counted = %v, want 1", got)
	}
}
