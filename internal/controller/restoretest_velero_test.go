package controller

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

const (
	veleroSchedule   = "velero.io/schedule-name"
	veleroScheduleWP = "wordpress"
)

// newVeleroBackup is a Velero Backup in the default Velero namespace.
func newVeleroBackup(name, phase string, labels map[string]string, included, excluded []string, done time.Time) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(veleroBackupGVK)
	obj.SetNamespace(defaultVeleroNamespace)
	obj.SetName(name)
	obj.SetLabels(labels)

	_ = unstructured.SetNestedStringSlice(obj.Object, included, "spec", "includedNamespaces")
	_ = unstructured.SetNestedStringSlice(obj.Object, excluded, "spec", "excludedNamespaces")
	_ = unstructured.SetNestedField(obj.Object, phase, "status", "phase")
	_ = unstructured.SetNestedField(obj.Object, done.UTC().Format(time.RFC3339), "status", "completionTimestamp")

	return obj
}

func scheduled(name string, minute int) *unstructured.Unstructured {
	return newVeleroBackup(name, veleroPhaseCompleted, map[string]string{veleroSchedule: veleroScheduleWP},
		nil, nil, time.Date(2026, 11, 15, 10, minute, 0, 0, time.UTC))
}

// veleroTwoSourceTest restores html and pgdata from the Backups of one Velero schedule.
func veleroTwoSourceTest() *restorev1alpha1.RestoreTest {
	sel := restorev1alpha1.BackupSelector{Labels: map[string]string{veleroSchedule: veleroScheduleWP}}

	test := volsyncTwoSourceTest()
	test.Spec.Provider = restorev1alpha1.ProviderVelero
	test.Spec.Sources[0].BackupSelector = sel
	test.Spec.Sources[1].BackupSelector = sel

	return test
}

func TestVeleroTestRunTwoSources(t *testing.T) {
	test := veleroTwoSourceTest()
	test.Spec.Triggers = &restorev1alpha1.Triggers{Manual: true}

	f := newRestoreTestFixture(t, test, scheduled("wp-1", 1), scheduled("wp-2", 2))
	f.setRunNow("n1")
	f.run()

	restores := f.restores()
	if len(restores) != 1 {
		t.Fatalf("restores = %d, want 1", len(restores))
	}

	got := restoreBackups(restores[0])
	if len(got) != 2 || got[testHTML] != "wp-2" || got[testPGData] != "wp-2" {
		t.Errorf("backupRefs = %v, want wp-2 for both sources", got)
	}

	if pins := asOf(restores[0]); len(pins) != 0 {
		t.Errorf("restoreAsOf = %v, want none for velero", pins)
	}
}

func TestVeleroBackupsCoverage(t *testing.T) {
	cases := []struct {
		name               string
		included, excluded []string
		want               bool
	}{
		{"all by default", nil, nil, true},
		{"wildcard", []string{"*"}, nil, true},
		{"named", []string{testNamespace}, nil, true},
		{"other namespace", []string{testOther}, nil, false},
		{"excluded", []string{"*"}, []string{testNamespace}, false},
		{"included glob", []string{"def*"}, nil, true},
		{"excluded glob", nil, []string{"def*"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := newVeleroBackup("b", veleroPhaseCompleted, nil, tc.included, tc.excluded, testStart)

			got := veleroBackups([]unstructured.Unstructured{*b}, restorev1alpha1.BackupSelector{Name: "b"}, testNamespace)
			if (len(got) == 1) != tc.want {
				t.Errorf("found = %v, want %v", len(got) == 1, tc.want)
			}
		})
	}
}

func TestVeleroBackupsPhase(t *testing.T) {
	for phase, want := range map[string]bool{
		"Completed": true, "PartiallyFailed": false, "InProgress": false, "Failed": false,
	} {
		t.Run(phase, func(t *testing.T) {
			b := newVeleroBackup("b", phase, nil, nil, nil, testStart)

			got := veleroBackups([]unstructured.Unstructured{*b}, restorev1alpha1.BackupSelector{Name: "b"}, testNamespace)
			if (len(got) == 1) != want {
				t.Errorf("found = %v, want %v", len(got) == 1, want)
			}
		})
	}
}

func TestVeleroTestAfterBackupOncePerBackup(t *testing.T) {
	test := veleroTwoSourceTest()
	test.Spec.Triggers = &restorev1alpha1.Triggers{AfterBackup: &restorev1alpha1.AfterBackupTrigger{}}

	f := newRestoreTestFixture(t, test, scheduled("wp-1", 1))
	f.now = time.Date(2026, 11, 15, 10, 20, 0, 0, time.UTC)
	f.run()
	f.finishRunning(restorev1alpha1.RestorePhaseSucceeded)
	f.run()

	if n := len(f.restores()); n != 1 {
		t.Fatalf("restores on one backup = %d, want 1", n)
	}

	last := f.test().Status.LastHandledBackups
	if len(last) != 2 || last[0].Name != "wp-1" || last[1].Name != "wp-1" {
		t.Errorf("lastHandledBackups = %v, want wp-1 for both sources", last)
	}

	if err := f.c.Create(f.ctx, scheduled("wp-2", 30)); err != nil {
		t.Fatal(err)
	}

	f.now = time.Date(2026, 11, 15, 10, 40, 0, 0, time.UTC)
	f.run()

	if n := len(f.restores()); n != 2 {
		t.Fatalf("restores after a new backup = %d, want 2", n)
	}
}

func TestVeleroTestUsesVeleroNamespace(t *testing.T) {
	test := veleroTwoSourceTest()
	test.Spec.Triggers = &restorev1alpha1.Triggers{Manual: true}

	moved := scheduled("wp-1", 1)
	moved.SetNamespace(testVeleroNS)

	f := newRestoreTestFixture(t, test, moved)
	f.setRunNow("n1")
	f.run()

	if n := len(f.restores()); n != 0 {
		t.Fatalf("restores with the default namespace = %d, want 0", n)
	}

	f.r.VeleroNamespace = testVeleroNS
	f.run()

	if n := len(f.restores()); n != 1 {
		t.Fatalf("restores with velero-system = %d, want 1", n)
	}
}
