package controller

import (
	"testing"
	"time"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

// A bad cron must not stop the afterBackup trigger.
func TestBadCronKeepsAfterBackup(t *testing.T) {
	pg := map[string]string{testLabelKey: testLabelValue}

	test := newRestoreTestObject()
	test.Spec.Sources[0].BackupSelector = restorev1alpha1.BackupSelector{Labels: pg}
	test.Spec.Triggers = &restorev1alpha1.Triggers{Cron: "61 * * * *", AfterBackup: &restorev1alpha1.AfterBackupTrigger{}}

	f := newRestoreTestFixture(t, test, newSnapshot("a", pg, "Succeeded", time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)))
	f.now = time.Date(2026, 11, 15, 10, 20, 0, 0, time.UTC)
	f.run()

	if n := len(f.restores()); n != 1 {
		t.Fatalf("restores = %d, want 1: the new backup was not tested because the cron is invalid", n)
	}
}
