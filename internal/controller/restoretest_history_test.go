package controller

import (
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

// keptFailure is the newest failed run, the only one the failure limit keeps.
const keptFailure = "bad-1"

// finishedRestore stores a Restore of the test t1 in a terminal phase, created at testStart plus minutes.
func finishedRestore(t *testing.T, f *testFixture, name string, phase restorev1alpha1.RestorePhase, minutes int) {
	t.Helper()

	restore := &restorev1alpha1.Restore{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         testNamespace,
			Labels:            map[string]string{testLabel: "t1"},
			CreationTimestamp: metav1.NewTime(testStart.Add(time.Duration(minutes) * time.Minute)),
		},
		Spec: newTestRestore().Spec,
	}

	if err := f.c.Create(f.ctx, restore); err != nil {
		t.Fatal(err)
	}

	restore.Status.Phase = phase

	if err := f.c.Status().Update(f.ctx, restore); err != nil {
		t.Fatal(err)
	}
}

func TestPruneHistoryKeepsNewestPerResult(t *testing.T) {
	test := newRestoreTestObject()
	one := int32(1)
	test.Spec.FailedRunsHistoryLimit = &one

	f := newRestoreTestFixture(t, test)

	for i := range 4 {
		finishedRestore(t, f, fmt.Sprintf("ok-%d", i), restorev1alpha1.RestorePhaseSucceeded, i)
	}

	finishedRestore(t, f, "bad-0", restorev1alpha1.RestorePhaseFailed, 0)
	finishedRestore(t, f, keptFailure, restorev1alpha1.RestorePhaseCanceled, 1)

	stored := f.test()
	stored.Status.RetainedResources = []string{"bad-0", keptFailure, "gone"}

	if err := f.r.pruneHistory(f.ctx, stored); err != nil {
		t.Fatal(err)
	}

	list := &restorev1alpha1.RestoreList{}
	if err := f.c.List(f.ctx, list, client.InNamespace(testNamespace)); err != nil {
		t.Fatal(err)
	}

	got := map[string]bool{}
	for _, r := range list.Items {
		got[r.Name] = true
	}

	want := []string{"ok-1", "ok-2", "ok-3", keptFailure}
	if len(got) != len(want) {
		t.Fatalf("restores = %v, want %v", got, want)
	}

	for _, name := range want {
		if !got[name] {
			t.Errorf("restore %s was pruned, want kept", name)
		}
	}

	if r := stored.Status.RetainedResources; len(r) != 1 || r[0] != keptFailure {
		t.Errorf("retainedResources = %v, want [bad-1]", r)
	}
}
