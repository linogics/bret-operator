package controller

import (
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

const (
	rsHTML = "rs-html"
	rsPG   = "rs-pgdata"
)

// newReplicationSource is a VolSync ReplicationSource. A nil lastSync means it never synced.
func newReplicationSource(name string, labels map[string]string, lastSync *time.Time) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(replicationSourceGVK)
	obj.SetNamespace(testNamespace)
	obj.SetName(name)
	obj.SetLabels(labels)

	if lastSync != nil {
		_ = unstructured.SetNestedField(obj.Object, lastSync.UTC().Format(time.RFC3339), "status", "lastSyncTime")
	}

	return obj
}

func at(minute int) *time.Time {
	t := time.Date(2026, 11, 15, 10, minute, 0, 0, time.UTC)

	return &t
}

// volsyncTwoSourceTest restores html and pgdata, each from its own ReplicationSource.
func volsyncTwoSourceTest() *restorev1alpha1.RestoreTest {
	test := newRestoreTestObject()
	test.Spec.Provider = restorev1alpha1.ProviderVolSync
	test.CreationTimestamp = metav1.NewTime(time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC))
	test.Spec.Sources = []restorev1alpha1.RestoreTestSource{
		{SourcePVC: testHTML, BackupSelector: restorev1alpha1.BackupSelector{Name: rsHTML}},
		{SourcePVC: testPGData, BackupSelector: restorev1alpha1.BackupSelector{Name: rsPG}},
	}

	return test
}

// setLastSync moves the lastSyncTime of a ReplicationSource, as a new VolSync backup does.
func (f *testFixture) setLastSync(name string, when *time.Time) {
	f.t.Helper()

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(replicationSourceGVK)

	if err := f.c.Get(f.ctx, client.ObjectKey{Namespace: testNamespace, Name: name}, obj); err != nil {
		f.t.Fatal(err)
	}

	_ = unstructured.SetNestedField(obj.Object, when.UTC().Format(time.RFC3339), "status", "lastSyncTime")

	if err := f.c.Update(f.ctx, obj); err != nil {
		f.t.Fatal(err)
	}
}

func asOf(r restorev1alpha1.Restore) map[string]time.Time {
	out := map[string]time.Time{}

	for _, s := range r.Spec.Sources {
		if s.RestoreAsOf != nil {
			out[s.SourcePVC] = s.RestoreAsOf.UTC()
		}
	}

	return out
}

func TestVolSyncTestManualRunTwoSources(t *testing.T) {
	test := volsyncTwoSourceTest()
	test.Spec.Triggers = &restorev1alpha1.Triggers{Manual: true}

	f := newRestoreTestFixture(t, test,
		newReplicationSource(rsHTML, nil, at(1)), newReplicationSource(rsPG, nil, at(2)))
	f.setRunNow("n1")
	f.run()

	restores := f.restores()
	if len(restores) != 1 || len(restores[0].Spec.Sources) != 2 {
		t.Fatalf("restores = %v, want one with two sources", restores)
	}

	pins := asOf(restores[0])
	if !pins[testHTML].Equal(*at(1)) || !pins[testPGData].Equal(*at(2)) {
		t.Errorf("restoreAsOf = %v, want html 10:01 and pgdata 10:02", pins)
	}

	if got := restoreBackups(restores[0]); got[testHTML] != rsHTML || got[testPGData] != rsPG {
		t.Errorf("backupRefs = %v, want the ReplicationSources", got)
	}
}

func TestVolSyncTestAfterBackupNewSync(t *testing.T) {
	test := volsyncTwoSourceTest()
	test.Spec.Triggers = &restorev1alpha1.Triggers{AfterBackup: &restorev1alpha1.AfterBackupTrigger{}}

	f := newRestoreTestFixture(t, test,
		newReplicationSource(rsHTML, nil, at(1)), newReplicationSource(rsPG, nil, at(2)))
	f.now = time.Date(2026, 11, 15, 10, 20, 0, 0, time.UTC)
	f.run()
	f.finishRunning(restorev1alpha1.RestorePhaseSucceeded)
	f.run()

	if n := len(f.restores()); n != 1 {
		t.Fatalf("restores on the same sync = %d, want 1", n)
	}

	f.setLastSync(rsHTML, at(30))
	f.setLastSync(rsPG, at(31))
	f.now = time.Date(2026, 11, 15, 10, 40, 0, 0, time.UTC)
	f.run()

	if n := len(f.restores()); n != 2 {
		t.Fatalf("restores after a new sync = %d, want 2", n)
	}
}

func TestVolSyncTestNeverSynced(t *testing.T) {
	test := volsyncTwoSourceTest()
	test.Spec.Triggers = &restorev1alpha1.Triggers{Manual: true}

	f := newRestoreTestFixture(t, test,
		newReplicationSource(rsHTML, nil, at(1)), newReplicationSource(rsPG, nil, nil))
	f.setRunNow("n1")
	f.run()

	if n := len(f.restores()); n != 0 {
		t.Fatalf("restores = %d, want 0", n)
	}

	if got := testReadyReason(f.test()); got != reasonBackupNotFound {
		t.Errorf("reason = %q, want %s", got, reasonBackupNotFound)
	}
}

func TestVolSyncTestAmbiguousSelector(t *testing.T) {
	test := volsyncTwoSourceTest()
	test.Spec.Triggers = &restorev1alpha1.Triggers{Manual: true}
	test.Spec.Sources[0].BackupSelector = restorev1alpha1.BackupSelector{Labels: map[string]string{testLabelKey: testLabelValue}}

	app := map[string]string{testLabelKey: testLabelValue}
	f := newRestoreTestFixture(t, test,
		newReplicationSource(rsHTML, app, at(1)), newReplicationSource(rsPG, app, at(2)))
	f.setRunNow("n1")
	f.run()

	if n := len(f.restores()); n != 0 {
		t.Fatalf("restores = %d, want 0", n)
	}

	if got := testReadyReason(f.test()); got != reasonBackupSelectorAmbiguous {
		t.Errorf("reason = %q, want %s", got, reasonBackupSelectorAmbiguous)
	}
}

func TestVolSyncTestQueuedRunKeepsPin(t *testing.T) {
	test := volsyncTwoSourceTest()
	test.Spec.Triggers = &restorev1alpha1.Triggers{Manual: true, AfterBackup: &restorev1alpha1.AfterBackupTrigger{}}
	test.Spec.OverlapPolicy = restorev1alpha1.OverlapPolicyQueue

	f := newRestoreTestFixture(t, test,
		newReplicationSource(rsHTML, nil, at(1)), newReplicationSource(rsPG, nil, at(2)))
	f.now = time.Date(2026, 11, 15, 10, 20, 0, 0, time.UTC)
	f.run()

	// A new sync lands while the first run is in progress, so it is queued with its time.
	f.setLastSync(rsHTML, at(21))
	f.setLastSync(rsPG, at(22))
	f.now = time.Date(2026, 11, 15, 10, 25, 0, 0, time.UTC)
	f.run()

	// Another sync lands before the queued run starts. The queued run keeps 10:21 and 10:22.
	f.setLastSync(rsHTML, at(26))
	f.setLastSync(rsPG, at(27))
	f.finishRunning(restorev1alpha1.RestorePhaseSucceeded)
	f.run()

	restores := f.restores()
	if len(restores) != 2 {
		t.Fatalf("restores = %d, want 2", len(restores))
	}

	checked := 0

	for _, r := range restores {
		if r.Labels[attemptLabel] == "" || isTerminal(r.Status.Phase) {
			continue
		}

		checked++

		pins := asOf(r)
		if !pins[testHTML].Equal(*at(21)) || !pins[testPGData].Equal(*at(22)) {
			t.Errorf("queued restoreAsOf = %v, want 10:21 and 10:22", pins)
		}
	}

	if checked != 1 {
		t.Errorf("queued restores checked = %d, want 1", checked)
	}
}

func TestVolSyncTestRetryKeepsPin(t *testing.T) {
	test := volsyncTwoSourceTest()
	test.Spec.Triggers = &restorev1alpha1.Triggers{Manual: true}
	test.Spec.Retries = restorev1alpha1.Retries{Count: 1, Backoff: metav1.Duration{Duration: time.Minute}}

	f := newRestoreTestFixture(t, test,
		newReplicationSource(rsHTML, nil, at(1)), newReplicationSource(rsPG, nil, at(2)))
	f.now = time.Date(2026, 11, 15, 10, 20, 0, 0, time.UTC)
	f.setRunNow("n1")
	f.run()
	f.finishRunning(restorev1alpha1.RestorePhaseFailed)
	f.run()

	// A new sync lands during the backoff. The retry keeps 10:01 and 10:02.
	f.setLastSync(rsHTML, at(21))
	f.setLastSync(rsPG, at(22))
	f.now = time.Date(2026, 11, 15, 10, 30, 0, 0, time.UTC)
	f.run()

	var retried *restorev1alpha1.Restore

	restores := f.restores()
	for i := range restores {
		if restores[i].Labels[attemptLabel] == "2" {
			retried = &restores[i]
		}
	}

	if retried == nil {
		t.Fatalf("no second attempt among %d restores", len(restores))
	}

	pins := asOf(*retried)
	if !pins[testHTML].Equal(*at(1)) || !pins[testPGData].Equal(*at(2)) {
		t.Errorf("retry restoreAsOf = %v, want 10:01 and 10:02", pins)
	}
}

func TestVolSyncTestReplaceKeepsRunWhenAmbiguous(t *testing.T) {
	test := volsyncTwoSourceTest()
	test.Spec.Triggers = &restorev1alpha1.Triggers{Manual: true}
	test.Spec.OverlapPolicy = restorev1alpha1.OverlapPolicyReplace
	test.Spec.Sources[0].BackupSelector = restorev1alpha1.BackupSelector{Labels: map[string]string{testLabelKey: testLabelValue}}

	app := map[string]string{testLabelKey: testLabelValue}
	f := newRestoreTestFixture(t, test,
		newReplicationSource(rsHTML, app, at(1)), newReplicationSource(rsPG, nil, at(2)))
	f.setRunNow("n1")
	f.run()

	running := f.restores()
	if len(running) != 1 {
		t.Fatalf("restores = %d, want 1", len(running))
	}

	// A second ReplicationSource gets the label, so the replacement cannot pick a backup.
	if err := f.c.Create(f.ctx, newReplicationSource("rs-other", app, at(3))); err != nil {
		t.Fatal(err)
	}

	f.setRunNow("n2")
	f.run()

	restores := f.restores()
	if len(restores) != 1 || restores[0].Name != running[0].Name {
		t.Errorf("restores = %d, want the running %s kept", len(restores), running[0].Name)
	}

	if got := testReadyReason(f.test()); got != reasonBackupSelectorAmbiguous {
		t.Errorf("reason = %q, want %s", got, reasonBackupSelectorAmbiguous)
	}
}

func TestVolSyncTestBackupNotFoundMessage(t *testing.T) {
	test := volsyncTwoSourceTest()
	test.Spec.Triggers = &restorev1alpha1.Triggers{Manual: true}

	f := newRestoreTestFixture(t, test,
		newReplicationSource(rsHTML, nil, at(1)), newReplicationSource(rsPG, nil, nil))
	f.setRunNow("n1")
	f.run()

	c := meta.FindStatusCondition(f.test().Status.Conditions, readyConditionType)
	if c == nil || strings.Contains(c.Message, "Succeeded") {
		t.Errorf("message = %v, want no Kopiur phase name", c)
	}
}
