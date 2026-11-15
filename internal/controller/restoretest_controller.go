package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

const (
	// runNowAnnotation starts one manual run each time its value changes.
	runNowAnnotation = "bret.linogics.io/run-now"
	testLabel        = "bret.linogics.io/test"
	runLabel         = "bret.linogics.io/run"
	attemptLabel     = "bret.linogics.io/attempt"
	// snapshotPollInterval is how often afterBackup looks for new snapshots. Snapshots are not watched.
	snapshotPollInterval   = time.Minute
	snapshotPhaseSucceeded = "Succeeded"
)

// Event and condition reasons for RestoreTest.
const (
	reasonRunSkipped     = "RunSkipped"
	reasonRunReplaced    = "RunReplaced"
	reasonRunning        = "Running"
	reasonBackupNotFound = "BackupNotFound"
	reasonWaitingBackups = "WaitingForBackups"

	conditionBackupsPending = "BackupsPending"
	reasonInvalidCron       = "InvalidCron"
)

// snapshotGVK is the Kopiur Snapshot kind. It is used as unstructured, so Kopiur is not a Go dependency.
var snapshotGVK = schema.GroupVersionKind{Group: kopiurGroup, Version: apiVersionAlpha1, Kind: "Snapshot"}

// RestoreTestReconciler reconciles a RestoreTest object. It starts Restore objects on its triggers and retries failed attempts.
type RestoreTestReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	// APIReader reads Kopiur Snapshot objects without starting an informer for them. Nil falls back to the client.
	APIReader client.Reader
	// Now returns the current time. Nil means time.Now.
	Now func() time.Time
	// EnabledProviders limits the providers this reconciler serves. Nil serves all of them.
	EnabledProviders map[restorev1alpha1.Provider]bool
}

// trigger is one due request for a run. consume records in the status that the request was handled.
type trigger struct {
	source string
	// backups maps sourcePVC to a backup. Empty means every source takes its newest backup.
	backups map[string]string
	consume func()
}

// completedBackup is a Succeeded Kopiur Snapshot that matches a backup selector.
type completedBackup struct {
	name      string
	completed time.Time
}

// +kubebuilder:rbac:groups=bret.linogics.io,resources=restoretests,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bret.linogics.io,resources=restoretests/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=bret.linogics.io,resources=restoretests/finalizers,verbs=update
// +kubebuilder:rbac:groups=bret.linogics.io,resources=restores,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bret.linogics.io,resources=restorechecks,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=kopiur.home-operations.com,resources=snapshots,verbs=get;list;watch

// Reconcile runs one step of the RestoreTest state machine and stores the status it produced.
func (r *RestoreTestReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	test := &restorev1alpha1.RestoreTest{}
	if err := r.Get(ctx, req.NamespacedName, test); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !test.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.finalize(ctx, test)
	}

	if controllerutil.AddFinalizer(test, metricsFinalizer) {
		if err := r.Update(ctx, test); err != nil {
			return ctrl.Result{}, err
		}
	}

	result, err := r.advance(ctx, test)

	// The gauge is set from status, so it survives an operator restart.
	if test.Status.LastSuccess != nil {
		restoreTestLastSuccess.WithLabelValues(test.Name, test.Namespace).Set(float64(test.Status.LastSuccess.Unix()))
	}

	if statusErr := r.Status().Update(ctx, test); statusErr != nil {
		return ctrl.Result{}, errors.Join(err, statusErr)
	}

	return result, err
}

// finalize removes the metric series of a deleted or renamed RestoreTest, then releases it.
func (r *RestoreTestReconciler) finalize(ctx context.Context, test *restorev1alpha1.RestoreTest) error {
	if !controllerutil.ContainsFinalizer(test, metricsFinalizer) {
		return nil
	}

	restoreTestLastSuccess.DeleteLabelValues(test.Name, test.Namespace)
	controllerutil.RemoveFinalizer(test, metricsFinalizer)

	return client.IgnoreNotFound(r.Update(ctx, test))
}

// advance settles a finished run, then starts or queues the next run when a trigger is due.
func (r *RestoreTestReconciler) advance(ctx context.Context, test *restorev1alpha1.RestoreTest) (ctrl.Result, error) {
	if test.Spec.Provider != restorev1alpha1.ProviderKopiur {
		r.blockTest(test, reasonProviderUnsupported, fmt.Sprintf("provider %s is not supported yet", test.Spec.Provider))

		return ctrl.Result{}, nil
	}

	// Tests read Kopiur snapshots. They need the kopiur provider and its rights.
	if r.EnabledProviders != nil && !r.EnabledProviders[restorev1alpha1.ProviderKopiur] {
		r.blockTest(test, reasonProviderUnsupported, "provider kopiur is disabled")

		return ctrl.Result{}, nil
	}

	if len(test.Spec.Sources) == 0 {
		r.blockTest(test, reasonSourceMissing, "the RestoreTest has no sources; recreate it in the new shape")

		return ctrl.Result{}, nil
	}

	restore, err := r.currentRestore(ctx, test)
	if err != nil {
		return ctrl.Result{}, err
	}

	if restore != nil && isTerminal(restore.Status.Phase) {
		var result ctrl.Result

		result, err = r.settle(ctx, test, restore)
		if err != nil || test.Status.CurrentRestore != "" {
			return result, err
		}
	}

	return r.runTriggers(ctx, test)
}

// currentRestore returns the Restore of the run in progress. It clears the reference when the Restore is gone.
func (r *RestoreTestReconciler) currentRestore(ctx context.Context, test *restorev1alpha1.RestoreTest) (*restorev1alpha1.Restore, error) {
	if test.Status.CurrentRestore == "" {
		return nil, nil
	}

	restore := &restorev1alpha1.Restore{}

	key := client.ObjectKey{Namespace: test.Namespace, Name: test.Status.CurrentRestore}

	err := r.Get(ctx, key, restore)
	if apierrors.IsNotFound(err) {
		// The cache can lag behind a Restore created in the last reconcile. Ask the API before dropping the run.
		err = r.reader().Get(ctx, key, restore)
	}

	if apierrors.IsNotFound(err) {
		test.Status.CurrentRestore = ""
		test.Status.RetryAt = nil

		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return restore, nil
}

// settle handles a terminal Restore of the run in progress. It schedules a retry, or records the final result.
func (r *RestoreTestReconciler) settle(
	ctx context.Context,
	test *restorev1alpha1.RestoreTest,
	restore *restorev1alpha1.Restore,
) (ctrl.Result, error) {
	attempt := attemptOf(restore)

	if restore.Status.Phase == restorev1alpha1.RestorePhaseFailed && attempt <= int(test.Spec.Retries.Count) {
		return r.retry(ctx, test, restore, attempt)
	}

	// The run is over, so the objects kept by its earlier attempts are no longer needed.
	if err := r.deleteEarlierAttempts(ctx, test, restore); err != nil {
		return ctrl.Result{}, err
	}

	now := r.now()
	result := restorev1alpha1.RunResultFailed
	status := metav1.ConditionFalse

	if restore.Status.Phase == restorev1alpha1.RestorePhaseSucceeded {
		result = restorev1alpha1.RunResultSucceeded
		status = metav1.ConditionTrue

		done := metav1.NewTime(now)
		test.Status.LastSuccess = &done
	}

	test.Status.LastRun = &restorev1alpha1.LastRun{
		Time:   metav1.NewTime(now),
		Result: result,
		Reason: restore.Status.Message,
	}

	if restore.Spec.KeepOnFailure && restore.Status.Phase != restorev1alpha1.RestorePhaseSucceeded {
		test.Status.RetainedResources = append(test.Status.RetainedResources, restore.Name)
	}

	test.Status.CurrentRestore = ""
	test.Status.RetryAt = nil

	setTestReady(test, status, string(result), restore.Status.Message)

	return ctrl.Result{}, nil
}

// deleteEarlierAttempts deletes the other Restores of the run. Their finalizer removes the retained objects.
func (r *RestoreTestReconciler) deleteEarlierAttempts(
	ctx context.Context,
	test *restorev1alpha1.RestoreTest,
	last *restorev1alpha1.Restore,
) error {
	list := &restorev1alpha1.RestoreList{}

	err := r.List(ctx, list, client.InNamespace(test.Namespace),
		client.MatchingLabels{testLabel: test.Name, runLabel: last.Labels[runLabel]})
	if err != nil {
		return err
	}

	for i := range list.Items {
		if list.Items[i].Name != last.Name {
			if err := client.IgnoreNotFound(r.Delete(ctx, &list.Items[i])); err != nil {
				return err
			}
		}
	}

	return nil
}

// retry waits for the backoff after a failed attempt, then creates the next attempt of the same run.
func (r *RestoreTestReconciler) retry(
	ctx context.Context,
	test *restorev1alpha1.RestoreTest,
	restore *restorev1alpha1.Restore,
	attempt int,
) (ctrl.Result, error) {
	now := r.now()

	if test.Status.RetryAt == nil {
		retryAt := metav1.NewTime(now.Add(test.Spec.Retries.Backoff.Duration))
		test.Status.RetryAt = &retryAt

		setTestReady(test, metav1.ConditionFalse, reasonRunning, fmt.Sprintf("attempt %d failed, retry scheduled", attempt))
	}

	if wait := test.Status.RetryAt.Sub(now); wait > 0 {
		return ctrl.Result{RequeueAfter: wait}, nil
	}

	// Keep the backups of the failed attempt, so a newer snapshot does not change the run.
	backups := make(map[string]string, len(restore.Spec.Sources))
	for _, src := range restore.Spec.Sources {
		backups[src.SourcePVC] = src.BackupRef.Name
	}

	err := r.createRestore(ctx, test, restore.Labels[runLabel], backups, attempt+1)

	return ctrl.Result{}, err
}

// runTriggers starts the queued run when no run is in progress, then handles one due trigger.
func (r *RestoreTestReconciler) runTriggers(ctx context.Context, test *restorev1alpha1.RestoreTest) (ctrl.Result, error) {
	if test.Status.CurrentRestore == "" && len(test.Status.PendingBackups) > 0 {
		started, err := r.start(ctx, test, test.Status.PendingRunSource, test.Status.PendingBackups)
		if err != nil {
			return ctrl.Result{}, err
		}

		if !started {
			if testReadyReason(test) == reasonBackupNotFound {
				return ctrl.Result{RequeueAfter: snapshotPollInterval}, nil
			}

			return ctrl.Result{}, nil
		}

		test.Status.PendingBackups = nil
		test.Status.PendingRunSource = ""

		return ctrl.Result{}, nil
	}

	trig, wait, err := r.nextTrigger(ctx, test)
	if err != nil {
		return ctrl.Result{}, err
	}

	if trig == nil {
		return ctrl.Result{RequeueAfter: wait}, nil
	}

	handled, err := r.handle(ctx, test, trig)
	if err != nil {
		return ctrl.Result{}, err
	}

	if !handled {
		if testReadyReason(test) == reasonBackupNotFound {
			return ctrl.Result{RequeueAfter: snapshotPollInterval}, nil
		}

		return ctrl.Result{}, nil
	}

	// Another trigger may be due. Check again right away.
	return ctrl.Result{Requeue: true}, nil //nolint:staticcheck // immediate requeue; RequeueAfter has no zero-delay form
}

// nextTrigger returns the first due trigger, or nil. It also returns how long to wait for the next one, zero for none.
func (r *RestoreTestReconciler) nextTrigger(ctx context.Context, test *restorev1alpha1.RestoreTest) (*trigger, time.Duration, error) {
	triggers := test.Spec.Triggers
	if triggers == nil || triggers.Cron == "" {
		test.Status.NextRun = nil
	}

	if triggers == nil {
		return nil, 0, nil
	}

	nonce := test.Annotations[runNowAnnotation]
	if triggers.Manual && nonce != "" && nonce != test.Status.LastRunNowValue {
		return &trigger{
			source:  "run-now-" + nonce,
			consume: func() { test.Status.LastRunNowValue = nonce },
		}, 0, nil
	}

	now := r.now()

	var wait time.Duration

	if triggers.Cron != "" {
		schedule, err := cron.ParseStandard(triggers.Cron)
		if err != nil {
			r.blockTest(test, reasonInvalidCron, fmt.Sprintf("parse cron %q: %v", triggers.Cron, err))

			return nil, 0, nil
		}

		next := metav1.NewTime(schedule.Next(now.UTC()))

		stale := test.Status.NextRun != nil && now.Before(test.Status.NextRun.Time) && !next.Time.Equal(test.Status.NextRun.Time)
		if test.Status.NextRun == nil || stale {
			test.Status.NextRun = &next
		}

		tick := test.Status.NextRun.Time
		if !now.Before(tick) {
			return &trigger{
				source: "cron-" + tick.UTC().Format(time.RFC3339),
				consume: func() {
					next := metav1.NewTime(schedule.Next(now.UTC()))
					test.Status.NextRun = &next
				},
			}, 0, nil
		}

		wait = tick.Sub(now)
	}

	if triggers.AfterBackup != nil {
		trig, backupWait, err := r.afterBackupTrigger(ctx, test, triggers.AfterBackup.Delay.Duration, now)
		if err != nil {
			return nil, 0, err
		}

		if trig != nil {
			return trig, 0, nil
		}

		wait = earliest(wait, earliest(backupWait, snapshotPollInterval))
	}

	return nil, wait, nil
}

// handledFor returns the last handled backup of a source, or nil.
func handledFor(test *restorev1alpha1.RestoreTest, sourcePVC string) *restorev1alpha1.HandledBackup {
	for i := range test.Status.LastHandledBackups {
		if test.Status.LastHandledBackups[i].SourcePVC == sourcePVC {
			return &test.Status.LastHandledBackups[i]
		}
	}

	return nil
}

// setHandled stores the last handled backup of its source.
func setHandled(test *restorev1alpha1.RestoreTest, h restorev1alpha1.HandledBackup) {
	for i := range test.Status.LastHandledBackups {
		if test.Status.LastHandledBackups[i].SourcePVC == h.SourcePVC {
			test.Status.LastHandledBackups[i] = h

			return
		}
	}

	test.Status.LastHandledBackups = append(test.Status.LastHandledBackups, h)
}

// isNewerThanWatermark reports whether b is strictly newer than the last handled backup.
// Equal times are ordered by name. completedBackups sorts the same way.
func isNewerThanWatermark(b completedBackup, wm *restorev1alpha1.HandledBackup) bool {
	if wm == nil {
		return true
	}

	if !b.completed.Equal(wm.CreatedAt.Time) {
		return b.completed.After(wm.CreatedAt.Time)
	}

	return b.name > wm.Name
}

// setBackupsPending reports sources that wait for a new backup while others have one.
// It leaves Ready alone, because staggered backups would flap it daily. With no new backup at all, there is nothing to report.
func (r *RestoreTestReconciler) setBackupsPending(test *restorev1alpha1.RestoreTest, waiting []string) {
	if len(waiting) == 0 || len(waiting) == len(test.Spec.Sources) {
		meta.RemoveStatusCondition(&test.Status.Conditions, conditionBackupsPending)

		return
	}

	msg := "waiting for a new backup of source(s) " + strings.Join(waiting, ", ")

	if c := meta.FindStatusCondition(test.Status.Conditions, conditionBackupsPending); c == nil || c.Message != msg {
		r.Recorder.Event(test, corev1.EventTypeNormal, reasonWaitingBackups, msg)
	}

	meta.SetStatusCondition(&test.Status.Conditions, metav1.Condition{
		Type:               conditionBackupsPending,
		Status:             metav1.ConditionTrue,
		Reason:             reasonWaitingBackups,
		Message:            msg,
		ObservedGeneration: test.Generation,
	})
}

// afterBackupTrigger fires when every source has a backup that no run handled and whose delay has passed.
// The run uses the newest backup of each source. Otherwise, it returns how long until all sources are due.
func (r *RestoreTestReconciler) afterBackupTrigger(
	ctx context.Context,
	test *restorev1alpha1.RestoreTest,
	delay time.Duration,
	now time.Time,
) (*trigger, time.Duration, error) {
	backups := make(map[string]string, len(test.Spec.Sources))
	used := make([]restorev1alpha1.HandledBackup, 0, len(test.Spec.Sources))
	names := make([]string, 0, len(test.Spec.Sources))

	var (
		due     time.Time
		waiting []string
	)

	for _, src := range test.Spec.Sources {
		list, err := r.completedBackups(ctx, test.Namespace, src.BackupSelector)
		if err != nil {
			return nil, 0, err
		}

		oldest := -1

		for i, b := range list {
			// Backups that finished before this test existed are not new. Testing them all at once would surprise the user.
			if isNewerThanWatermark(b, handledFor(test, src.SourcePVC)) && !b.completed.Before(test.CreationTimestamp.Time) {
				oldest = i

				break
			}
		}

		if oldest < 0 {
			waiting = append(waiting, src.SourcePVC)

			continue
		}

		if d := list[oldest].completed.Add(delay); d.After(due) {
			due = d
		}

		newest := list[len(list)-1]
		backups[src.SourcePVC] = newest.name
		names = append(names, newest.name)
		used = append(used, restorev1alpha1.HandledBackup{SourcePVC: src.SourcePVC, Name: newest.name, CreatedAt: metav1.NewTime(newest.completed)})
	}

	r.setBackupsPending(test, waiting)

	if len(waiting) > 0 {
		return nil, 0, nil
	}

	if now.Before(due) {
		return nil, due.Sub(now), nil
	}

	return &trigger{
		source:  "backup-" + strings.Join(names, "+"),
		backups: backups,
		consume: func() {
			for _, h := range used {
				setHandled(test, h)
			}
		},
	}, 0, nil
}

// handle applies the overlap policy to a due trigger. It reports false when the trigger was not used and must wait.
func (r *RestoreTestReconciler) handle(ctx context.Context, test *restorev1alpha1.RestoreTest, trig *trigger) (bool, error) {
	if test.Status.CurrentRestore == "" {
		started, err := r.start(ctx, test, trig.source, trig.backups)
		if err != nil || !started {
			return false, err
		}

		trig.consume()

		return true, nil
	}

	switch test.Spec.OverlapPolicy {
	case restorev1alpha1.OverlapPolicyQueue:
		return r.queue(ctx, test, trig)
	case restorev1alpha1.OverlapPolicyReplace:
		return r.replace(ctx, test, trig)
	default:
		r.Recorder.Event(test, corev1.EventTypeNormal, reasonRunSkipped, "run "+trig.source+" skipped, a run is in progress")
		trig.consume()

		return true, nil
	}
}

// queue keeps one pending run. The pending run waits behind the run in progress. A trigger that finds a pending run is skipped.
func (r *RestoreTestReconciler) queue(ctx context.Context, test *restorev1alpha1.RestoreTest, trig *trigger) (bool, error) {
	if len(test.Status.PendingBackups) > 0 {
		r.Recorder.Event(test, corev1.EventTypeNormal, reasonRunSkipped, "run "+trig.source+" skipped, a run is already pending")
		trig.consume()

		return true, nil
	}

	backups, ok, err := r.resolveOrBlock(ctx, test, trig.backups)
	if err != nil || !ok {
		return false, err
	}

	test.Status.PendingBackups = backups
	test.Status.PendingRunSource = "queued-" + trig.source

	trig.consume()

	return true, nil
}

// replace deletes the run in progress and starts the trigger in its place.
func (r *RestoreTestReconciler) replace(ctx context.Context, test *restorev1alpha1.RestoreTest, trig *trigger) (bool, error) {
	old := &restorev1alpha1.Restore{}

	err := r.Get(ctx, client.ObjectKey{Namespace: test.Namespace, Name: test.Status.CurrentRestore}, old)
	if client.IgnoreNotFound(err) != nil {
		return false, err
	}

	if err == nil {
		if err = r.deleteEarlierAttempts(ctx, test, old); err != nil {
			return false, err
		}

		if err = client.IgnoreNotFound(r.Delete(ctx, old)); err != nil {
			return false, err
		}
	}

	r.Recorder.Event(test, corev1.EventTypeNormal, reasonRunReplaced, "replaced run "+test.Status.CurrentRestore+" with "+trig.source)

	test.Status.CurrentRestore = ""
	test.Status.RetryAt = nil
	test.Status.PendingBackups = nil
	test.Status.PendingRunSource = ""

	started, err := r.start(ctx, test, trig.source, trig.backups)
	if err != nil || !started {
		return false, err
	}

	trig.consume()

	return true, nil
}

// start creates the first attempt of a new run. Sources without a preferred backup take their newest one.
func (r *RestoreTestReconciler) start(ctx context.Context, test *restorev1alpha1.RestoreTest, source string, preferred map[string]string) (bool, error) {
	backups, ok, err := r.resolveOrBlock(ctx, test, preferred)
	if err != nil || !ok {
		return false, err
	}

	err = r.createRestore(ctx, test, startRunID(test, source), backups, 1)
	if err != nil {
		return false, err
	}

	return true, nil
}

// resolveOrBlock resolves the backups of every source. It reports false and sets BackupNotFound when a source has none.
func (r *RestoreTestReconciler) resolveOrBlock(
	ctx context.Context,
	test *restorev1alpha1.RestoreTest,
	preferred map[string]string,
) (map[string]string, bool, error) {
	backups, missing, err := r.resolveBackups(ctx, test, preferred)
	if err != nil {
		return nil, false, err
	}

	if len(missing) > 0 {
		r.blockTest(test, reasonBackupNotFound, "no Succeeded backup matches backupSelector for source(s) "+strings.Join(missing, ", "))

		return nil, false, nil
	}

	return backups, true, nil
}

// createRestore creates the Restore of one attempt and makes it the run in progress.
// An existing attempt is accepted. It can come from an earlier reconcile that did not store its status.
func (r *RestoreTestReconciler) createRestore(ctx context.Context, test *restorev1alpha1.RestoreTest, run string, backups map[string]string, attempt int) error {
	sources := make([]restorev1alpha1.RestoreSource, 0, len(test.Spec.Sources))
	for _, src := range test.Spec.Sources {
		sources = append(sources, restorev1alpha1.RestoreSource{
			SourcePVC:     src.SourcePVC,
			BackupRef:     restorev1alpha1.BackupRef{Name: backups[src.SourcePVC], Namespace: test.Namespace},
			RestoreTarget: src.RestoreTarget,
		})
	}

	restore := &restorev1alpha1.Restore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s-a%d", test.Name, run, attempt),
			Namespace: test.Namespace,
			Labels: map[string]string{
				testLabel:    test.Name,
				runLabel:     run,
				attemptLabel: strconv.Itoa(attempt),
			},
		},
		Spec: restorev1alpha1.RestoreSpec{
			Provider:        test.Spec.Provider,
			Sources:         sources,
			RestoreCheckRef: test.Spec.RestoreCheckRef,
			RestoreWindow:   test.Spec.RestoreWindow,
			KeepOnFailure:   test.Spec.KeepOnFailure,
		},
	}

	err := controllerutil.SetControllerReference(test, restore, r.Scheme)
	if err != nil {
		return err
	}

	err = r.Create(ctx, restore)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	test.Status.CurrentRestore = restore.Name
	test.Status.RetryAt = nil

	setTestReady(test, metav1.ConditionFalse, reasonRunning, "attempt "+strconv.Itoa(attempt)+" started")

	return nil
}

// resolveBackups picks one backup per source: preferred, else the selector name, else the newest Succeeded one.
// Sources without a backup are returned in missing.
func (r *RestoreTestReconciler) resolveBackups(
	ctx context.Context,
	test *restorev1alpha1.RestoreTest,
	preferred map[string]string,
) (map[string]string, []string, error) {
	backups := make(map[string]string, len(test.Spec.Sources))

	var missing []string

	for _, src := range test.Spec.Sources {
		switch {
		case preferred[src.SourcePVC] != "":
			backups[src.SourcePVC] = preferred[src.SourcePVC]
		case src.BackupSelector.Name != "":
			backups[src.SourcePVC] = src.BackupSelector.Name
		default:
			list, err := r.completedBackups(ctx, test.Namespace, src.BackupSelector)
			if err != nil {
				return nil, nil, err
			}

			if len(list) == 0 {
				missing = append(missing, src.SourcePVC)

				continue
			}

			backups[src.SourcePVC] = list[len(list)-1].name
		}
	}

	return backups, missing, nil
}

// completedBackups lists the Succeeded Kopiur Snapshots that match the backup selector, oldest first.
func (r *RestoreTestReconciler) completedBackups(ctx context.Context, namespace string, sel restorev1alpha1.BackupSelector) ([]completedBackup, error) {
	snapshots := &unstructured.UnstructuredList{}
	snapshots.SetGroupVersionKind(snapshotGVK.GroupVersion().WithKind("SnapshotList"))

	err := r.reader().List(ctx, snapshots, client.InNamespace(namespace))
	if err != nil {
		return nil, err
	}

	var backups []completedBackup

	for i := range snapshots.Items {
		obj := &snapshots.Items[i]

		if kopiurPhase(obj) != snapshotPhaseSucceeded || !selects(sel, obj) {
			continue
		}

		completed, ok := completionTime(obj)
		if !ok {
			continue
		}

		backups = append(backups, completedBackup{name: obj.GetName(), completed: completed})
	}

	sort.Slice(backups, func(i, j int) bool {
		if backups[i].completed.Equal(backups[j].completed) {
			return backups[i].name < backups[j].name
		}

		return backups[i].completed.Before(backups[j].completed)
	})

	return backups, nil
}

// blockTest sets Ready=False with a reason and emits a warning event when the reason changes.
func (r *RestoreTestReconciler) blockTest(test *restorev1alpha1.RestoreTest, reason, msg string) {
	if testReadyReason(test) != reason {
		r.Recorder.Event(test, corev1.EventTypeWarning, reason, msg)
	}

	setTestReady(test, metav1.ConditionFalse, reason, msg)
}

func setTestReady(test *restorev1alpha1.RestoreTest, status metav1.ConditionStatus, reason, msg string) {
	meta.SetStatusCondition(&test.Status.Conditions, metav1.Condition{
		Type:               readyConditionType,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: test.Generation,
	})
}

func testReadyReason(test *restorev1alpha1.RestoreTest) string {
	condition := meta.FindStatusCondition(test.Status.Conditions, readyConditionType)
	if condition == nil {
		return ""
	}

	return condition.Reason
}

// selects reports whether a backup object matches the backup selector.
func selects(sel restorev1alpha1.BackupSelector, obj *unstructured.Unstructured) bool {
	if sel.Name != "" {
		return obj.GetName() == sel.Name
	}

	if len(sel.Labels) == 0 {
		return false
	}

	for k, v := range sel.Labels {
		if obj.GetLabels()[k] != v {
			return false
		}
	}

	return true
}

// completionTime is the lastTransitionTime of the Ready=True condition of a Kopiur Snapshot.
func completionTime(obj *unstructured.Unstructured) (time.Time, bool) {
	conditions, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")

	for _, c := range conditions {
		condition, ok := c.(map[string]any)
		if !ok || condition["type"] != "Ready" || condition["status"] != "True" {
			continue
		}

		raw, _ := condition["lastTransitionTime"].(string)

		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return time.Time{}, false
		}

		return t, true
	}

	return time.Time{}, false
}

// attemptOf reads the attempt number of a Restore. A missing or invalid label counts as the first attempt.
func attemptOf(restore *restorev1alpha1.Restore) int {
	attempt, err := strconv.Atoi(restore.Labels[attemptLabel])
	if err != nil {
		return 1
	}

	return attempt
}

// runID is 8 hex characters of the hash of a trigger source. It names the Restore objects of one run.
func runID(source string) string {
	sum := sha256.Sum256([]byte(source))

	return hex.EncodeToString(sum[:4])
}

// startRunID is runID of the source and the time of the last finished run. A source that comes back later
// gets a new name. While a run is starting, LastRun does not change, so the name stays stable.
func startRunID(test *restorev1alpha1.RestoreTest, source string) string {
	if test.Status.LastRun == nil {
		return runID(source)
	}

	return runID(source + "@" + test.Status.LastRun.Time.UTC().Format(time.RFC3339))
}

// earliest returns the shorter of two waits, where zero means no wait is needed.
func earliest(a, b time.Duration) time.Duration {
	if a == 0 || (b != 0 && b < a) {
		return b
	}

	return a
}

func (r *RestoreTestReconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}

	return time.Now()
}

// reader reads through the API reader when one is set, so it starts no informer. It is used for Kopiur Snapshots and cache-miss lookups.
func (r *RestoreTestReconciler) reader() client.Reader {
	if r.APIReader != nil {
		return r.APIReader
	}

	return r.Client
}

// SetupWithManager sets up the controller with the Manager.
func (r *RestoreTestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&restorev1alpha1.RestoreTest{}).
		Owns(&restorev1alpha1.Restore{}).
		Named("restoretest").
		Complete(r)
}
