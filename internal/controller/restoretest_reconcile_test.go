package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

var testStart = time.Date(2026, 11, 15, 10, 30, 0, 0, time.UTC)

// testFixture runs a RestoreTestReconciler against the fake client with a clock the test controls.
type testFixture struct {
	t   *testing.T
	ctx context.Context
	c   client.Client
	r   *RestoreTestReconciler
	rec *record.FakeRecorder
	now time.Time
	key types.NamespacedName
}

func newRestoreTestFixture(t *testing.T, test *restorev1alpha1.RestoreTest, objs ...client.Object) *testFixture {
	t.Helper()

	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}

	if err := restorev1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}

	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(append(objs, test)...).
		WithStatusSubresource(&restorev1alpha1.RestoreTest{}, &restorev1alpha1.Restore{}).
		Build()

	f := &testFixture{
		t:   t,
		ctx: context.Background(),
		c:   c,
		rec: record.NewFakeRecorder(100),
		now: testStart,
		key: client.ObjectKeyFromObject(test),
	}
	f.r = &RestoreTestReconciler{
		Client:   c,
		Scheme:   s,
		Recorder: f.rec,
		Now:      func() time.Time { return f.now },
	}

	return f
}

func newRestoreTestObject() *restorev1alpha1.RestoreTest {
	return &restorev1alpha1.RestoreTest{
		ObjectMeta: metav1.ObjectMeta{Name: "t1", Namespace: testNamespace},
		Spec: restorev1alpha1.RestoreTestSpec{
			Provider:        restorev1alpha1.ProviderKopiur,
			BackupSelector:  restorev1alpha1.BackupSelector{Name: "b1"},
			RestoreCheckRef: restorev1alpha1.RestoreCheckRef{Name: testApp},
			Target:          restorev1alpha1.RestoreTarget{Size: quantityPtr("1Gi")},
			RestoreWindow:   metav1.Duration{Duration: 45 * time.Minute},
			OverlapPolicy:   restorev1alpha1.OverlapPolicySkip,
		},
	}
}

// run reconciles until the reconciler stops asking for an immediate requeue.
func (f *testFixture) run() {
	f.t.Helper()

	for range 10 {
		res, err := f.r.Reconcile(f.ctx, ctrl.Request{NamespacedName: f.key})
		if err != nil {
			f.t.Fatal(err)
		}

		if !res.Requeue { //nolint:staticcheck // immediate requeue check, see controller
			return
		}
	}

	f.t.Fatal("reconcile did not settle")
}

func (f *testFixture) test() *restorev1alpha1.RestoreTest {
	f.t.Helper()

	test := &restorev1alpha1.RestoreTest{}
	if err := f.c.Get(f.ctx, f.key, test); err != nil {
		f.t.Fatal(err)
	}

	return test
}

// setRunNow changes the run-now annotation of the test.
func (f *testFixture) setRunNow(value string) {
	f.t.Helper()

	test := f.test()
	if test.Annotations == nil {
		test.Annotations = map[string]string{}
	}

	test.Annotations[runNowAnnotation] = value

	if err := f.c.Update(f.ctx, test); err != nil {
		f.t.Fatal(err)
	}
}

func (f *testFixture) restores() []restorev1alpha1.Restore {
	f.t.Helper()

	list := &restorev1alpha1.RestoreList{}
	if err := f.c.List(f.ctx, list, client.InNamespace(testNamespace)); err != nil {
		f.t.Fatal(err)
	}

	return list.Items
}

// finishRunning sets the phase of the Restore that is not terminal yet.
func (f *testFixture) finishRunning(phase restorev1alpha1.RestorePhase) {
	f.t.Helper()

	restores := f.restores()
	for i := range restores {
		if isTerminal(restores[i].Status.Phase) {
			continue
		}

		restores[i].Status.Phase = phase
		restores[i].Status.Message = "done by test"

		if err := f.c.Status().Update(f.ctx, &restores[i]); err != nil {
			f.t.Fatal(err)
		}

		return
	}

	f.t.Fatal("no running restore to finish")
}

// events drains the recorded events as "type reason message" lines.
func (f *testFixture) events() []string {
	var out []string

	for {
		select {
		case e := <-f.rec.Events:
			out = append(out, e)
		default:
			return out
		}
	}
}

func hasEvent(events []string, reason string) bool {
	for _, e := range events {
		if strings.Contains(e, " "+reason+" ") {
			return true
		}
	}

	return false
}

func newSnapshot(name string, labels map[string]string, phase string, ready time.Time) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kopiur.home-operations.com/v1alpha1",
		"kind":       "Snapshot",
		"metadata":   map[string]any{"name": name, "namespace": testNamespace},
		testStatusKey: map[string]any{
			"phase": phase,
			"conditions": []any{map[string]any{
				"type":               "Ready",
				testStatusKey:        "True",
				"lastTransitionTime": ready.UTC().Format(time.RFC3339),
			}},
		},
	}}
	obj.SetLabels(labels)

	return obj
}

func TestRestoreTestCronCreatesRestore(t *testing.T) {
	test := newRestoreTestObject()
	test.Spec.Triggers = &restorev1alpha1.Triggers{Cron: "0 * * * *"}

	f := newRestoreTestFixture(t, test)

	f.run()

	if n := len(f.restores()); n != 0 {
		t.Fatalf("restores before the first tick = %d, want 0", n)
	}

	f.now = time.Date(2026, 11, 15, 11, 0, 1, 0, time.UTC)
	f.run()

	restores := f.restores()
	if len(restores) != 1 {
		t.Fatalf("restores after the tick = %d, want 1", len(restores))
	}

	if got := restores[0].Labels[testLabel]; got != "t1" {
		t.Errorf("test label = %q, want t1", got)
	}

	if got := restores[0].Labels[attemptLabel]; got != "1" {
		t.Errorf("attempt label = %q, want 1", got)
	}

	if !strings.HasSuffix(restores[0].Name, "-a1") || !strings.HasPrefix(restores[0].Name, "t1-") {
		t.Errorf("restore name = %q, want t1-<id>-a1", restores[0].Name)
	}

	if got := restores[0].Spec.Source.BackupRef.Name; got != "b1" {
		t.Errorf("backupRef name = %q, want b1", got)
	}

	next := f.test().Status.NextRun
	if next == nil || !next.Time.Equal(time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("nextRun = %v, want 12:00", next)
	}
}

func TestRestoreTestAfterBackupOneRunPerSnapshot(t *testing.T) {
	test := newRestoreTestObject()
	test.Spec.BackupSelector = restorev1alpha1.BackupSelector{Labels: map[string]string{testLabelKey: testLabelValue}}
	test.Spec.Triggers = &restorev1alpha1.Triggers{AfterBackup: &restorev1alpha1.AfterBackupTrigger{}}
	test.Spec.OverlapPolicy = restorev1alpha1.OverlapPolicyQueue

	pg := map[string]string{testLabelKey: testLabelValue}
	objs := []client.Object{
		newSnapshot("a", pg, "Succeeded", time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)),
		newSnapshot("b", pg, "Succeeded", time.Date(2026, 11, 15, 10, 10, 0, 0, time.UTC)),
		newSnapshot("c", pg, "Failed", time.Date(2026, 11, 15, 10, 12, 0, 0, time.UTC)),
		newSnapshot("d", map[string]string{testLabelKey: "other"}, "Succeeded", time.Date(2026, 11, 15, 10, 13, 0, 0, time.UTC)),
	}

	f := newRestoreTestFixture(t, test, objs...)
	f.now = time.Date(2026, 11, 15, 10, 20, 0, 0, time.UTC)

	f.run()

	if n := len(f.restores()); n != 1 {
		t.Fatalf("restores after first run = %d, want 1 (oldest snapshot)", n)
	}

	f.finishRunning(restorev1alpha1.RestorePhaseSucceeded)
	f.run()

	if n := len(f.restores()); n != 2 {
		t.Fatalf("restores after second run = %d, want 2", n)
	}

	f.run()

	if n := len(f.restores()); n != 2 {
		t.Fatalf("restores after idle reconcile = %d, want 2", n)
	}

	last := f.test().Status.LastHandled
	if last == nil || last.Name != "b" {
		t.Errorf("lastHandled = %v, want b", last)
	}
}

func TestRestoreTestManualNonceRunsOnce(t *testing.T) {
	test := newRestoreTestObject()
	test.Spec.Triggers = &restorev1alpha1.Triggers{Manual: true}

	f := newRestoreTestFixture(t, test)
	f.setRunNow("n1")
	f.run()

	if n := len(f.restores()); n != 1 {
		t.Fatalf("restores after nonce = %d, want 1", n)
	}

	f.finishRunning(restorev1alpha1.RestorePhaseSucceeded)
	f.run()
	f.run()

	if n := len(f.restores()); n != 1 {
		t.Fatalf("restores after reused nonce = %d, want 1", n)
	}

	status := f.test().Status
	if status.LastRun == nil || status.LastRun.Result != restorev1alpha1.RunResultSucceeded {
		t.Errorf("lastRun = %+v, want Succeeded", status.LastRun)
	}

	if status.LastRunNowValue != "n1" {
		t.Errorf("lastRunNowValue = %q, want n1", status.LastRunNowValue)
	}
}

func TestRestoreTestSkipDropsTrigger(t *testing.T) {
	test := newRestoreTestObject()
	test.Spec.Triggers = &restorev1alpha1.Triggers{Manual: true}
	test.Spec.OverlapPolicy = restorev1alpha1.OverlapPolicySkip

	f := newRestoreTestFixture(t, test)
	f.setRunNow("n1")
	f.run()
	f.setRunNow("n2")
	f.run()

	if n := len(f.restores()); n != 1 {
		t.Fatalf("restores = %d, want 1", n)
	}

	if !hasEvent(f.events(), reasonRunSkipped) {
		t.Errorf("missing %s event", reasonRunSkipped)
	}

	if got := f.test().Status.LastRunNowValue; got != "n2" {
		t.Errorf("lastRunNowValue = %q, want n2 (trigger consumed)", got)
	}
}

func TestRestoreTestQueueStartsPendingRunAfterCurrent(t *testing.T) {
	test := newRestoreTestObject()
	test.Spec.Triggers = &restorev1alpha1.Triggers{Manual: true}
	test.Spec.OverlapPolicy = restorev1alpha1.OverlapPolicyQueue

	f := newRestoreTestFixture(t, test)
	f.setRunNow("n1")
	f.run()
	f.setRunNow("n2")
	f.run()

	if n := len(f.restores()); n != 1 {
		t.Fatalf("restores while queued = %d, want 1", n)
	}

	if got := f.test().Status.PendingRun; got != "b1" {
		t.Fatalf("pendingRun = %q, want b1", got)
	}

	f.finishRunning(restorev1alpha1.RestorePhaseSucceeded)
	f.run()

	if n := len(f.restores()); n != 2 {
		t.Fatalf("restores after current run ended = %d, want 2", n)
	}

	if got := f.test().Status.PendingRun; got != "" {
		t.Errorf("pendingRun = %q, want empty", got)
	}
}

func TestRestoreTestReplaceDeletesRunning(t *testing.T) {
	test := newRestoreTestObject()
	test.Spec.Triggers = &restorev1alpha1.Triggers{Manual: true}
	test.Spec.OverlapPolicy = restorev1alpha1.OverlapPolicyReplace

	f := newRestoreTestFixture(t, test)
	f.setRunNow("n1")
	f.run()

	first := f.restores()[0].Name

	f.setRunNow("n2")
	f.run()

	restores := f.restores()
	if len(restores) != 1 {
		t.Fatalf("restores after replace = %d, want 1", len(restores))
	}

	if restores[0].Name == first {
		t.Errorf("replacement reuses name %q", first)
	}

	if !hasEvent(f.events(), reasonRunReplaced) {
		t.Errorf("missing %s event", reasonRunReplaced)
	}
}

func TestRestoreTestRetryAfterBackoffOnly(t *testing.T) {
	test := newRestoreTestObject()
	test.Spec.Triggers = &restorev1alpha1.Triggers{Manual: true}
	test.Spec.Retries = restorev1alpha1.Retries{Count: 1, Backoff: metav1.Duration{Duration: 5 * time.Minute}}

	f := newRestoreTestFixture(t, test)
	f.setRunNow("n1")
	f.run()
	f.finishRunning(restorev1alpha1.RestorePhaseFailed)

	f.run()

	if n := len(f.restores()); n != 1 {
		t.Fatalf("restores during backoff = %d, want 1", n)
	}

	f.now = testStart.Add(4 * time.Minute)
	f.run()

	if n := len(f.restores()); n != 1 {
		t.Fatalf("restores before backoff ends = %d, want 1", n)
	}

	f.now = testStart.Add(5 * time.Minute)
	f.run()

	restores := f.restores()
	if len(restores) != 2 {
		t.Fatalf("restores after backoff = %d, want 2", len(restores))
	}

	var attempt2 bool

	for _, restore := range restores {
		if restore.Labels[attemptLabel] == "2" && strings.HasSuffix(restore.Name, "-a2") {
			attempt2 = true
		}
	}

	if !attempt2 {
		t.Error("no attempt 2 Restore with attempt label 2")
	}
}

func TestRestoreTestTargetMissing(t *testing.T) {
	test := newRestoreTestObject()
	test.Spec.Target = restorev1alpha1.RestoreTarget{}
	test.Spec.Triggers = &restorev1alpha1.Triggers{Manual: true}

	f := newRestoreTestFixture(t, test)
	f.setRunNow("n1")
	f.run()

	if n := len(f.restores()); n != 0 {
		t.Fatalf("restores = %d, want 0", n)
	}

	condition := apimeta.FindStatusCondition(f.test().Status.Conditions, readyConditionType)
	if condition == nil || condition.Status != metav1.ConditionFalse || condition.Reason != reasonTargetMissing {
		t.Errorf("Ready condition = %+v, want False %s", condition, reasonTargetMissing)
	}
}

func TestRestoreTestAfterBackupIgnoresBackupsBeforeCreation(t *testing.T) {
	test := newRestoreTestObject()
	test.CreationTimestamp = metav1.NewTime(time.Date(2026, 11, 15, 10, 5, 0, 0, time.UTC))
	test.Spec.BackupSelector = restorev1alpha1.BackupSelector{Labels: map[string]string{testLabelKey: testLabelValue}}
	test.Spec.Triggers = &restorev1alpha1.Triggers{AfterBackup: &restorev1alpha1.AfterBackupTrigger{}}

	pg := map[string]string{testLabelKey: testLabelValue}
	objs := []client.Object{
		newSnapshot("old", pg, "Succeeded", time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)),
		newSnapshot("new", pg, "Succeeded", time.Date(2026, 11, 15, 10, 10, 0, 0, time.UTC)),
	}

	f := newRestoreTestFixture(t, test, objs...)
	f.now = time.Date(2026, 11, 15, 10, 20, 0, 0, time.UTC)

	f.run()

	restores := f.restores()
	if len(restores) != 1 {
		t.Fatalf("restores = %d, want 1 (only the backup after creation)", len(restores))
	}

	if got := restores[0].Spec.Source.BackupRef.Name; got != "new" {
		t.Errorf("restore source = %q, want new", got)
	}
}

func TestRestoreTestKopiurDisabled(t *testing.T) {
	test := newRestoreTestObject()
	test.Spec.BackupSelector = restorev1alpha1.BackupSelector{Labels: map[string]string{testLabelKey: testLabelValue}}
	test.Spec.Triggers = &restorev1alpha1.Triggers{AfterBackup: &restorev1alpha1.AfterBackupTrigger{}}

	snap := newSnapshot("a", map[string]string{testLabelKey: testLabelValue}, "Succeeded",
		time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC))

	f := newRestoreTestFixture(t, test, snap)
	f.now = time.Date(2026, 11, 15, 10, 20, 0, 0, time.UTC)
	f.r.EnabledProviders = map[restorev1alpha1.Provider]bool{restorev1alpha1.ProviderVelero: true}

	res, err := f.r.Reconcile(f.ctx, ctrl.Request{NamespacedName: f.key})
	if err != nil || res.Requeue || res.RequeueAfter != 0 { //nolint:staticcheck // requeue check
		t.Fatalf("result = %+v, err = %v, want no requeue", res, err)
	}

	if n := len(f.restores()); n != 0 {
		t.Fatalf("restores = %d, want 0", n)
	}

	if reason := testReadyReason(f.test()); reason != reasonProviderUnsupported {
		t.Errorf("Ready reason = %q, want %s", reason, reasonProviderUnsupported)
	}
}

func TestWatermarkTieBreaksOnName(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	wm := &restorev1alpha1.HandledBackup{Name: "a", CreatedAt: metav1.NewTime(at)}

	tests := []struct {
		name string
		b    completedBackup
		wm   *restorev1alpha1.HandledBackup
		want bool
	}{
		{"same time, greater name", completedBackup{name: "b", completed: at}, wm, true},
		{"same backup", completedBackup{name: "a", completed: at}, wm, false},
		{"same time, lesser name", completedBackup{name: "0", completed: at}, wm, false},
		{"older", completedBackup{name: "z", completed: at.Add(-time.Second)}, wm, false},
		{"newer", completedBackup{name: "0", completed: at.Add(time.Second)}, wm, true},
		{"no watermark", completedBackup{name: "a", completed: at}, nil, true},
	}

	for _, tt := range tests {
		if got := isNewerThanWatermark(tt.b, tt.wm); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}
