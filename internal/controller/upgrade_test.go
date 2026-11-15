package controller

import (
	"testing"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

func TestRestoreTestStatusDecodesOldShape(t *testing.T) {
	s := runtime.NewScheme()
	if err := restorev1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}

	raw := `{"apiVersion":"bret.linogics.io/v1alpha1","kind":"RestoreTest","metadata":{"name":"a"},` +
		`"status":{"lastHandled":{"name":"a","createdAt":"2026-01-01T00:00:00Z"}}}`

	if _, _, err := serializer.NewCodecFactory(s).UniversalDeserializer().Decode([]byte(raw), nil, nil); err != nil {
		t.Fatalf("decode old shape: %v", err)
	}
}

func TestRestoreWithoutSourcesFails(t *testing.T) {
	restore := newTestRestore()
	restore.Spec.Sources = nil
	c, r := fakeRestoreClient(t, restore, newTestCheck())
	key := client.ObjectKeyFromObject(restore)

	reconcileRestore(t, r, key)
	reconcileRestore(t, r, key)

	got := getRestore(t, c, key)
	if got.Status.Phase != restorev1alpha1.RestorePhaseFailed || readyReason(got) != reasonSourceMissing {
		t.Fatalf("phase = %q, reason = %q, want Failed/%s", got.Status.Phase, readyReason(got), reasonSourceMissing)
	}

	if got.Status.Message != "the Restore has no sources; recreate it in the new shape" {
		t.Errorf("message = %q", got.Status.Message)
	}
}

func TestRestoreTestWithoutSourcesBlocks(t *testing.T) {
	test := newRestoreTestObject()
	test.Spec.Sources = nil
	test.Spec.Triggers = &restorev1alpha1.Triggers{Manual: true}

	f := newRestoreTestFixture(t, test)
	f.setRunNow("1")
	f.run()

	if n := len(f.restores()); n != 0 {
		t.Fatalf("restores = %d, want 0", n)
	}

	if got := testReadyReason(f.test()); got != reasonSourceMissing {
		t.Errorf("Ready reason = %q, want %s", got, reasonSourceMissing)
	}
}

func TestBackupsPendingKeepsReady(t *testing.T) {
	test := twoSourceTest()
	test.Spec.Triggers = &restorev1alpha1.Triggers{AfterBackup: &restorev1alpha1.AfterBackupTrigger{}}
	test.CreationTimestamp.Time = testStart.AddDate(0, 0, -1)

	f := newRestoreTestFixture(t, test, snapAt("h1", testHTML, 10), snapAt("p1", testPGData, 11))
	f.now = testStart
	f.run()
	f.finishRunning(restorev1alpha1.RestorePhaseSucceeded)
	f.run()

	if got := testReadyReason(f.test()); got != string(restorev1alpha1.RunResultSucceeded) {
		t.Fatalf("reason after run = %q, want %s", got, string(restorev1alpha1.RunResultSucceeded))
	}

	if err := f.c.Create(f.ctx, snapAt("h2", testHTML, 20)); err != nil {
		t.Fatal(err)
	}

	f.run()

	got := f.test()
	if r := testReadyReason(got); r != string(restorev1alpha1.RunResultSucceeded) {
		t.Errorf("Ready reason = %q, want %s", r, string(restorev1alpha1.RunResultSucceeded))
	}

	if c := apimeta.FindStatusCondition(got.Status.Conditions, conditionBackupsPending); c == nil || c.Status != "True" {
		t.Errorf("BackupsPending = %+v, want True", c)
	}
}
