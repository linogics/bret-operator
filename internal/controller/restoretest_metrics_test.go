package controller

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

// The success series exists while the RestoreTest exists, and is removed when it is deleted.
func TestRestoreTestSuccessSeriesFollowsObject(t *testing.T) {
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}

	if err := restorev1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}

	done := metav1.Now()
	test := &restorev1alpha1.RestoreTest{
		ObjectMeta: metav1.ObjectMeta{Name: "metric-test", Namespace: "metric-ns"},
		Status:     restorev1alpha1.RestoreTestStatus{LastSuccess: &done},
	}

	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(test).
		WithStatusSubresource(&restorev1alpha1.RestoreTest{}).
		Build()

	r := &RestoreTestReconciler{Client: c, Scheme: s, Recorder: record.NewFakeRecorder(10)}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(test)}
	ctx := context.Background()

	before := testutil.CollectAndCount(restoreTestLastSuccess)

	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if got := testutil.CollectAndCount(restoreTestLastSuccess); got != before+1 {
		t.Fatalf("series after reconcile: %d, want %d", got, before+1)
	}

	stored := &restorev1alpha1.RestoreTest{}
	if err := c.Get(ctx, req.NamespacedName, stored); err != nil {
		t.Fatal(err)
	}

	if err := c.Delete(ctx, stored); err != nil {
		t.Fatal(err)
	}

	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("reconcile after delete: %v", err)
	}

	if got := testutil.CollectAndCount(restoreTestLastSuccess); got != before {
		t.Fatalf("series after delete: %d, want %d", got, before)
	}
}

func TestPollIntervalDefault(t *testing.T) {
	t.Parallel()

	if got := (&RestoreReconciler{}).pollInterval(); got != defaultPollInterval {
		t.Errorf("default poll interval %v, want %v", got, defaultPollInterval)
	}

	r := &RestoreReconciler{PollInterval: 0}
	r.PollInterval = 7_000_000_000

	if got := r.pollInterval(); got != 7_000_000_000 {
		t.Errorf("configured poll interval %v, want 7s", got)
	}
}
