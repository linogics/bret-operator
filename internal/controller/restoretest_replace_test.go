package controller

import (
	"testing"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

// A VolSync afterBackup run replaced by a newer sync must start a live Restore.
func TestVolSyncReplaceWaitsForOldRestore(t *testing.T) {
	test := volsyncTwoSourceTest()
	test.Spec.Triggers = &restorev1alpha1.Triggers{AfterBackup: &restorev1alpha1.AfterBackupTrigger{}}
	test.Spec.OverlapPolicy = restorev1alpha1.OverlapPolicyReplace

	f := newRestoreTestFixture(t, test,
		newReplicationSource(rsHTML, nil, at(1)), newReplicationSource(rsPG, nil, at(2)))
	f.now = time.Date(2026, 11, 15, 10, 20, 0, 0, time.UTC)
	f.run()

	// The Restore controller puts its cleanup finalizer on every Restore.
	running := f.restores()[0]
	controllerutil.AddFinalizer(&running, cleanupFinalizer)

	if err := f.c.Update(f.ctx, &running); err != nil {
		t.Fatal(err)
	}

	f.setLastSync(rsHTML, at(30))
	f.setLastSync(rsPG, at(31))
	f.now = time.Date(2026, 11, 15, 10, 40, 0, 0, time.UTC)
	f.run()

	// The replacement waits while the replaced Restore of the same name is cleaned up.
	if cur := f.test().Status.CurrentRestore; cur != "" {
		t.Fatalf("current %q, want none while the replaced Restore is being deleted", cur)
	}

	dying := f.restores()[0]
	controllerutil.RemoveFinalizer(&dying, cleanupFinalizer)

	if err := f.c.Update(f.ctx, &dying); err != nil {
		t.Fatal(err)
	}

	f.run()

	cur := f.test().Status.CurrentRestore
	for _, r := range f.restores() {
		if r.Name == cur && r.DeletionTimestamp.IsZero() && asOf(r)[testHTML].Equal(*at(30)) {
			return
		}
	}

	t.Fatalf("replacement run for the 10:30 sync did not start, current %q", cur)
}
