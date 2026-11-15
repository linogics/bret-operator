package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

// countingReader counts the Get calls for Kopiur Snapshots.
type countingReader struct {
	client.Reader
	snapshots int
}

func (c *countingReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if u, ok := obj.(*unstructured.Unstructured); ok && u.GetKind() == snapshotGVK.Kind {
		c.snapshots++
	}

	return c.Reader.Get(ctx, key, obj, opts...)
}

func TestSourceSizeIsResolvedOnce(t *testing.T) {
	restore := newTestRestore()
	restore.Spec.Sources[0].Size = nil
	c, r := fakeRestoreClient(t, restore, newTestCheck(), kopiurSnapshotOfSize(2<<30))
	counter := &countingReader{Reader: c}
	r.APIReader = counter
	key := client.ObjectKeyFromObject(restore)

	reconcileRestore(t, r, key)
	reconcileRestore(t, r, key)

	reads := counter.snapshots

	reconcileRestore(t, r, key)
	reconcileRestore(t, r, key)

	if counter.snapshots != reads {
		t.Fatalf("snapshot read again after the size was stored: %d, want %d", counter.snapshots, reads)
	}

	if got, ok := getRestore(t, c, key).Status.ResolvedSizes[testDataName]; !ok || got.Value() != (2<<30)*110/100 {
		t.Fatalf("resolvedSizes = %v", got)
	}
}

func TestVolSyncMoverUIDFromCheck(t *testing.T) {
	uid := int64(999)
	obj := volsyncDestinationObject(volsyncTestRestore(), "repo", *quantityPtr(testSize1Gi), &uid)

	got, _, _ := unstructured.NestedInt64(obj.Object, "spec", "restic", "moverSecurityContext", "runAsUser")
	if got != 999 {
		t.Fatalf("runAsUser = %d, want 999", got)
	}

	restore := volsyncTestRestore()
	bp := newTestCheck()
	bp.Annotations = map[string]string{moverUIDAnnotation: `{"data": 70}`}
	c, r := fakeRestoreClient(t, restore, bp, volsyncSource(), volsyncSecret())

	reconcileToSizing(t, r, client.ObjectKeyFromObject(restore))

	rd, err := volsyncDestination(t, c, restore)
	if err != nil {
		t.Fatal(err)
	}

	if got, _, _ := unstructured.NestedInt64(rd.Object, "spec", "restic", "moverSecurityContext", "fsGroup"); got != 70 {
		t.Fatalf("fsGroup = %d, want 70", got)
	}
}

func TestVolSyncRepositoryMissing(t *testing.T) {
	restore := volsyncTestRestore()
	src := volsyncSource()
	unstructured.RemoveNestedField(src.Object, "spec", "restic")
	c, r := fakeRestoreClient(t, restore, newTestCheck(), src)
	key := client.ObjectKeyFromObject(restore)

	reconcileRestore(t, r, key)

	res, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: key})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	got := getRestore(t, c, key)
	if got.Status.Phase != restorev1alpha1.RestorePhaseFailed {
		t.Fatalf("phase %q, want Failed", got.Status.Phase)
	}

	if reason := readyReason(got); reason != reasonRepositoryMissing {
		t.Fatalf("reason %q, want %s", reason, reasonRepositoryMissing)
	}

	if res.RequeueAfter != 0 {
		t.Fatalf("RequeueAfter %v, want 0", res.RequeueAfter)
	}

	if _, err := volsyncDestination(t, c, restore); !errors.IsNotFound(err) {
		t.Fatalf("ReplicationDestination created without repository: %v", err)
	}
}

func TestVolSyncSecretMissing(t *testing.T) {
	restore := volsyncTestRestore()
	c, r := fakeRestoreClient(t, restore, newTestCheck(), volsyncSource())

	reconcileToSizing(t, r, client.ObjectKeyFromObject(restore))

	if reason := readyReason(getRestore(t, c, client.ObjectKeyFromObject(restore))); reason != reasonRepositoryMissing {
		t.Fatalf("reason %q, want %s", reason, reasonRepositoryMissing)
	}

	if _, err := volsyncDestination(t, c, restore); !errors.IsNotFound(err) {
		t.Fatalf("ReplicationDestination created without repository secret: %v", err)
	}
}

func TestVeleroNamesAreUniquePerNamespace(t *testing.T) {
	a := newTestRestore()
	b := newTestRestore()
	b.Namespace = testOther

	if veleroScratchName(a) == veleroScratchName(b) || veleroName(a) == veleroName(b) {
		t.Fatal("Restores with the same name in two namespaces share names")
	}

	long := newTestRestore()
	long.Name = "a-very-long-restore-name-that-would-break-a-namespace-name-if-it-were-used-directly-here"

	if n := len(veleroScratchName(long)); n > 63 {
		t.Fatalf("scratch namespace name has %d characters", n)
	}
}

func TestVeleroUsesConfiguredNamespace(t *testing.T) {
	restore := newTestRestore()
	r := &RestoreReconciler{VeleroNamespace: "velero-system"}

	if got := r.veleroStub(restore).GetNamespace(); got != "velero-system" {
		t.Fatalf("namespace %q, want velero-system", got)
	}

	if got := (&RestoreReconciler{}).veleroStub(restore).GetNamespace(); got != "velero" {
		t.Fatalf("default namespace %q, want velero", got)
	}
}

func TestVeleroCopiesPodSecurityLabels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(pvcTarGz(t, map[string]string{testNamespace + "/data": testSize1Gi}))
	}))
	defer srv.Close()

	restore := newTestRestore()
	restore.Spec.Provider = restorev1alpha1.ProviderVelero

	tenant := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace, Labels: map[string]string{
		"pod-security.kubernetes.io/enforce": "restricted", "team": "a",
	}}}
	c, r := fakeRestoreClient(t, restore, newTestCheck(), tenant)
	key := client.ObjectKeyFromObject(restore)

	go func() {
		for ctx.Err() == nil {
			answerDownloadRequest(ctx, c, srv.URL)
		}
	}()

	reconcileRestore(t, r, key)
	reconcileRestore(t, r, key)

	vr := r.veleroStub(restore)
	if err := c.Get(ctx, client.ObjectKeyFromObject(vr), vr); err != nil {
		t.Fatal(err)
	}

	if err := unstructured.SetNestedField(vr.Object, "Completed", "status", "phase"); err != nil {
		t.Fatal(err)
	}

	if err := c.Update(ctx, vr); err != nil {
		t.Fatal(err)
	}

	scratch := veleroScratchNamespace(restore)
	if err := c.Create(ctx, scratch.DeepCopy()); err != nil {
		t.Fatal(err)
	}

	if err := c.Create(ctx, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: testDataName, Namespace: scratch.Name}}); err != nil {
		t.Fatal(err)
	}

	reconcileRestore(t, r, key)

	if err := c.Get(ctx, client.ObjectKeyFromObject(scratch), scratch); err != nil {
		t.Fatal(err)
	}

	if scratch.Labels["pod-security.kubernetes.io/enforce"] != "restricted" || scratch.Labels["team"] != "" {
		t.Fatalf("scratch labels %v, want only the pod-security labels", scratch.Labels)
	}

	pod := &corev1.Pod{}
	if err := c.Get(ctx, client.ObjectKey{Name: checkPodName(restore), Namespace: scratch.Name}, pod); err != nil {
		t.Fatalf("check pod not created after the labels: %v", err)
	}
}

func TestVeleroDeleteRunningRestoreCleansUp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(pvcTarGz(t, map[string]string{testNamespace + "/data": testSize1Gi}))
	}))
	defer srv.Close()

	restore := newTestRestore()
	restore.Spec.Provider = restorev1alpha1.ProviderVelero
	restore.Spec.KeepOnFailure = true

	tenant := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}
	c, r := fakeRestoreClient(t, restore, newTestCheck(), tenant)
	key := client.ObjectKeyFromObject(restore)

	go func() {
		for ctx.Err() == nil {
			answerDownloadRequest(ctx, c, srv.URL)
		}
	}()

	reconcileRestore(t, r, key)
	reconcileRestore(t, r, key)

	scratch := veleroScratchNamespace(restore)
	if err := c.Create(ctx, scratch.DeepCopy()); err != nil {
		t.Fatal(err)
	}

	if err := c.Delete(ctx, getRestore(t, c, key)); err != nil {
		t.Fatal(err)
	}

	reconcileRestore(t, r, key)

	if err := c.Get(ctx, client.ObjectKeyFromObject(scratch), scratch); !errors.IsNotFound(err) {
		t.Fatalf("scratch namespace left behind: %v", err)
	}

	vr := r.veleroStub(restore)
	if err := c.Get(ctx, client.ObjectKeyFromObject(vr), vr); !errors.IsNotFound(err) {
		t.Fatalf("velero restore left behind: %v", err)
	}

	if err := c.Get(ctx, key, &restorev1alpha1.Restore{}); !errors.IsNotFound(err) {
		t.Fatalf("restore not released: %v", err)
	}
}

func TestRestoreTestDeletesEarlierAttempts(t *testing.T) {
	test := newRestoreTestObject()
	test.Spec.Triggers = &restorev1alpha1.Triggers{Manual: true}
	test.Spec.KeepOnFailure = true
	test.Spec.Retries = restorev1alpha1.Retries{Count: 1, Backoff: metav1.Duration{Duration: time.Minute}}

	f := newRestoreTestFixture(t, test)
	f.setRunNow("n1")
	f.run()
	f.finishRunning(restorev1alpha1.RestorePhaseFailed)
	f.run()

	f.now = testStart.Add(time.Minute)
	f.run()
	f.finishRunning(restorev1alpha1.RestorePhaseFailed)
	f.run()

	restores := f.restores()
	if len(restores) != 1 || restores[0].Labels[attemptLabel] != "2" {
		t.Fatalf("restores after the run settled = %d, want only attempt 2", len(restores))
	}
}

// deleteWithBrokenReads reconciles a Restore once, deletes it and reconciles again while reads of the
// kinds that match fail with err. The Restore must be released.
func deleteWithBrokenReads(t *testing.T, provider restorev1alpha1.Provider, kind string, err error) {
	t.Helper()

	restore := newTestRestore()
	restore.Spec.Provider = provider

	c, r := fakeRestoreClient(t, restore, newTestCheck(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}})
	key := client.ObjectKeyFromObject(restore)

	reconcileRestore(t, r, key)

	if delErr := c.Delete(context.Background(), getRestore(t, c, key)); delErr != nil {
		t.Fatal(delErr)
	}

	r.Client = interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
		List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if u, ok := list.(*unstructured.UnstructuredList); ok && u.GetKind() == kind+"List" {
				return err
			}

			return cl.List(ctx, list, opts...)
		},
		Get: func(ctx context.Context, cl client.WithWatch, k client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if u, ok := obj.(*unstructured.Unstructured); ok && u.GetKind() == kind {
				return err
			}

			return cl.Get(ctx, k, obj, opts...)
		},
	})

	reconcileRestore(t, r, key)

	if getErr := c.Get(context.Background(), key, &restorev1alpha1.Restore{}); !errors.IsNotFound(getErr) {
		t.Fatalf("restore stuck terminating: %v", getErr)
	}
}

func TestFinalizerIgnoresMissingCRD(t *testing.T) {
	deleteWithBrokenReads(t, restorev1alpha1.ProviderKopiur, kopiurRestoreGVK.Kind,
		&meta.NoKindMatchError{GroupKind: kopiurRestoreGVK.GroupKind(), SearchedVersions: []string{"v1alpha1"}})
}

func TestFinalizerIgnoresForbiddenVelero(t *testing.T) {
	deleteWithBrokenReads(t, restorev1alpha1.ProviderVelero, veleroRestoreGVK.Kind,
		errors.NewForbidden(schema.GroupResource{Group: "velero.io", Resource: "restores"}, "x", nil))
}

func TestFinalizerSkipsDisabledProvider(t *testing.T) {
	restore := newTestRestore()
	c, r := fakeRestoreClient(t, restore, newTestCheck())
	r.EnabledProviders = map[restorev1alpha1.Provider]bool{restorev1alpha1.ProviderVolSync: true}
	key := client.ObjectKeyFromObject(restore)

	kopiur := kopiurStub(restore)
	if err := c.Create(context.Background(), kopiurRestoreObject(restore, restore.Spec.Sources[0], nil)); err != nil {
		t.Fatal(err)
	}

	if err := r.cleanup(context.Background(), getRestore(t, c, key)); err != nil {
		t.Fatal(err)
	}

	if err := c.Get(context.Background(), client.ObjectKeyFromObject(kopiur), kopiur); err != nil {
		t.Fatalf("disabled provider object was touched: %v", err)
	}
}

func TestPreUpgradeRestoreWithoutSizeFails(t *testing.T) {
	restore := newTestRestore()
	c, r := fakeRestoreClient(t, restore, newTestCheck())
	key := client.ObjectKeyFromObject(restore)

	stored := getRestore(t, c, key)
	started := metav1.NewTime(time.Now())
	deadline := metav1.NewTime(started.Add(time.Hour))
	stored.Status = restorev1alpha1.RestoreStatus{
		Phase: restorev1alpha1.RestorePhaseRunning, Attempt: 1, StartTime: &started, Deadline: &deadline,
		ProviderRefs: []restorev1alpha1.ProviderRef{{Kind: "Restore", Name: "r1-kopiur-" + testDataName, Namespace: testNamespace}},
	}

	if err := c.Status().Update(context.Background(), stored); err != nil {
		t.Fatal(err)
	}

	reconcileRestore(t, r, key)

	got := getRestore(t, c, key)
	if got.Status.Phase != restorev1alpha1.RestorePhaseFailed || readyReason(got) != restorev1alpha1.ReasonSizeUnknown {
		t.Fatalf("phase %q reason %q, want Failed SizeUnknown", got.Status.Phase, readyReason(got))
	}

	pvcs := &corev1.PersistentVolumeClaimList{}
	if err := c.List(context.Background(), pvcs); err != nil || len(pvcs.Items) != 0 {
		t.Fatalf("PVCs created: %v %d", err, len(pvcs.Items))
	}
}
