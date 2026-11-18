package controller

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

// A check pod that succeeded before the deadline must give Succeeded, even when the
// reconcile that sees it comes after the deadline.
func TestCheckResultCountsAfterDeadline(t *testing.T) {
	ctx := context.Background()
	restore := newTestRestore()
	c, r := fakeRestoreClient(t, restore, newTestCheck())
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

	reconcileRestore(t, r, key) // check pod created

	pod := &corev1.Pod{}
	if err := c.Get(ctx, types.NamespacedName{Name: checkPodName(restore), Namespace: restore.Namespace}, pod); err != nil {
		t.Fatal(err)
	}

	pod.Status.Phase = corev1.PodSucceeded
	if err := c.Status().Update(ctx, pod); err != nil {
		t.Fatal(err)
	}

	// The pod finished in time, the next poll lands just after the deadline.
	stored := getRestore(t, c, key)
	past := metav1.NewTime(time.Now().Add(-time.Second))
	stored.Status.Deadline = &past

	if err := c.Status().Update(ctx, stored); err != nil {
		t.Fatal(err)
	}

	reconcileRestore(t, r, key)

	if got := getRestore(t, c, key).Status.Phase; got != restorev1alpha1.RestorePhaseSucceeded {
		t.Fatalf("phase %q, want Succeeded: the check pod had already passed", got)
	}
}
