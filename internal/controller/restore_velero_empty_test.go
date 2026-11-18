package controller

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"sigs.k8s.io/controller-runtime/pkg/client"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

// A Completed Velero Restore that restored nothing (no scratch namespace) must fail,
// not wait for the window.
func TestVeleroZeroItemsFails(t *testing.T) {
	c, r, restore := completedVelero(t, newTestCheck())

	ctx := context.Background()

	if err := c.Delete(ctx, veleroScratchNamespace(restore)); err != nil {
		t.Fatal(err)
	}

	// Velero reports a Completed restore of zero items.
	vr := r.veleroStub(restore)
	if err := c.Get(ctx, client.ObjectKeyFromObject(vr), vr); err != nil {
		t.Fatal(err)
	}

	if err := unstructured.SetNestedField(vr.Object, int64(0), "status", "progress", "itemsRestored"); err != nil {
		t.Fatal(err)
	}

	if err := c.Update(ctx, vr); err != nil {
		t.Fatal(err)
	}

	key := client.ObjectKeyFromObject(restore)
	reconcileRestore(t, r, key)

	if got := getRestore(t, c, key).Status.Phase; got != restorev1alpha1.RestorePhaseFailed {
		t.Fatalf("phase %q, want Failed: Velero completed without creating the scratch namespace", got)
	}
}
