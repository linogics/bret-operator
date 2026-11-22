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
	restoreBusyPoll        = 10 * time.Second
	snapshotPhaseSucceeded = "Succeeded"
)

// Event and condition reasons for RestoreTest.
const (
	reasonRunSkipped              = "RunSkipped"
	reasonRunReplaced             = "RunReplaced"
	reasonRunning                 = "Running"
	reasonBackupNotFound          = "BackupNotFound"
	reasonWaitingBackups          = "WaitingForBackups"
	reasonBackupSelectorAmbiguous = "BackupSelectorAmbiguous"
	reasonRestoreBusy             = "RestoreNameInUse"

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
	// APIReader reads backup objects without starting an informer for them. Nil falls back to the client.
	APIReader client.Reader
	// VeleroNamespace is the Velero install namespace, where Velero Backups live. Empty means "velero".
	VeleroNamespace string
	// Now returns the current time. Nil means time.Now.
	Now func() time.Time
	// EnabledProviders limits the providers this reconciler serves. Nil serves all of them.
	EnabledProviders map[restorev1alpha1.Provider]bool
}

// trigger is one due request for a run. consume records in the status that the request was handled.
type trigger struct {
	source string
	// backups maps sourcePVC to a backup. Empty means every source takes its newest backup.
	backups map[string]restorev1alpha1.HandledBackup
	consume func()
}

// completedBackup is a finished backup that matches a backup selector.
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
// +kubebuilder:rbac:groups=velero.io,resources=backups,verbs=get;list;watch

// Reconcile runs one step of the RestoreTest state machine and stores the status it produced.
func (r *RestoreTestReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	test := &restorev1alpha1.RestoreTest{}
	if err := r.Get(ctx, req.NamespacedName, test); err != nil {
		if apierrors.IsNotFound(err) {
			// The test is gone, so its metric series goes too.
			restoreTestLastSuccess.DeleteLabelValues(req.Name, req.Namespace)
		}

		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Older releases added a finalizer. Drop it so the test is not blocked when the operator is gone.
	if controllerutil.RemoveFinalizer(test, legacyMetricsFinalizer) {
		if err := r.Update(ctx, test); err != nil {
			return ctrl.Result{}, err
		}
	}

	if !test.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	result, err := r.advance(ctx, test)
	if errors.Is(err, errRestoreBusy) {
		setTestReady(test, metav1.ConditionFalse, reasonRestoreBusy, err.Error())

		result, err = ctrl.Result{RequeueAfter: restoreBusyPoll}, nil
	}

	// The gauge is set from status, so it survives an operator restart.
	if test.Status.LastSuccess != nil {
		restoreTestLastSuccess.WithLabelValues(test.Name, test.Namespace).Set(float64(test.Status.LastSuccess.Unix()))
	}

	if statusErr := r.Status().Update(ctx, test); statusErr != nil {
		return ctrl.Result{}, errors.Join(err, statusErr)
	}

	return result, err
}

// advance settles a finished run, then starts or queues the next run when a trigger is due.
func (r *RestoreTestReconciler) advance(ctx context.Context, test *restorev1alpha1.RestoreTest) (ctrl.Result, error) {
	// Tests read the backup objects of their provider. They need the provider and its rights.
	if !providerOn(r.EnabledProviders, test.Spec.Provider) {
		r.blockTest(test, reasonProviderUnsupported, fmt.Sprintf("provider %s is disabled", test.Spec.Provider))

		return ctrl.Result{}, nil
	}

	if len(test.Spec.Sources) == 0 {
		r.blockTest(test, reasonSourceMissing, "the RestoreTest has no sources. Recreate it in the new shape")

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

	result, err := r.runTriggers(ctx, test)
	if errors.Is(err, errBackupSelectorAmbiguous) {
		r.blockTest(test, reasonBackupSelectorAmbiguous, err.Error())

		return ctrl.Result{RequeueAfter: snapshotPollInterval}, nil
	}

	return result, err
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

	return ctrl.Result{}, r.pruneHistory(ctx, test)
}

// defaultHistoryLimit matches the CRD default. Objects built in Go carry no CRD default.
const defaultHistoryLimit = 3

// historyLimit is the limit, or the default when unset.
func historyLimit(limit *int32) int {
	if limit == nil {
		return defaultHistoryLimit
	}

	return int(*limit)
}

// pruneHistory deletes the Restores of finished runs beyond the history limits, oldest first.
// Their finalizer removes the objects they retained. RetainedResources keeps only Restores that remain.
func (r *RestoreTestReconciler) pruneHistory(ctx context.Context, test *restorev1alpha1.RestoreTest) error {
	list := &restorev1alpha1.RestoreList{}

	err := r.List(ctx, list, client.InNamespace(test.Namespace), client.MatchingLabels{testLabel: test.Name})
	if err != nil {
		return err
	}

	var passed, failed []*restorev1alpha1.Restore

	for i := range list.Items {
		restore := &list.Items[i]

		if restore.Name == test.Status.CurrentRestore || !isTerminal(restore.Status.Phase) || !restore.DeletionTimestamp.IsZero() {
			continue
		}

		if restore.Status.Phase == restorev1alpha1.RestorePhaseSucceeded {
			passed = append(passed, restore)
		} else {
			failed = append(failed, restore)
		}
	}

	kept := map[string]bool{}

	for _, group := range []struct {
		restores []*restorev1alpha1.Restore
		limit    int
	}{
		{passed, historyLimit(test.Spec.SuccessfulRunsHistoryLimit)},
		{failed, historyLimit(test.Spec.FailedRunsHistoryLimit)},
	} {
		sort.Slice(group.restores, func(i, j int) bool {
			a, b := group.restores[i], group.restores[j]
			if !a.CreationTimestamp.Equal(&b.CreationTimestamp) {
				return a.CreationTimestamp.After(b.CreationTimestamp.Time)
			}

			return a.Name > b.Name
		})

		for i, restore := range group.restores {
			if i < group.limit {
				kept[restore.Name] = true

				continue
			}

			if err := client.IgnoreNotFound(r.Delete(ctx, restore)); err != nil {
				return err
			}
		}
	}

	retained := test.Status.RetainedResources[:0]

	for _, name := range test.Status.RetainedResources {
		if kept[name] {
			retained = append(retained, name)
		}
	}

	test.Status.RetainedResources = retained

	return nil
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
	backups := make(map[string]restorev1alpha1.HandledBackup, len(restore.Spec.Sources))
	for _, src := range restore.Spec.Sources {
		b := restorev1alpha1.HandledBackup{SourcePVC: src.SourcePVC, Name: src.BackupRef.Name}
		if src.RestoreAsOf != nil {
			b.CreatedAt = *src.RestoreAsOf
		}

		backups[src.SourcePVC] = b
	}

	err := r.createRestore(ctx, test, restore.Labels[runLabel], backups, attempt+1)

	return ctrl.Result{}, err
}

// runTriggers starts the queued run when no run is in progress, then handles one due trigger.
func (r *RestoreTestReconciler) runTriggers(ctx context.Context, test *restorev1alpha1.RestoreTest) (ctrl.Result, error) {
	if test.Status.CurrentRestore == "" && len(test.Status.PendingBackups) > 0 {
		started, err := r.start(ctx, test, test.Status.PendingRunSource, pendingMap(test.Status.PendingBackups))
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
	return ctrl.Result{Requeue: true}, nil //nolint:staticcheck // immediate requeue, RequeueAfter has no zero-delay form
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

	var schedule cron.Schedule

	if triggers.Cron != "" {
		var err error

		schedule, err = cron.ParseStandard(triggers.Cron)
		if err != nil {
			// A bad cron must not stop the afterBackup trigger.
			r.blockTest(test, reasonInvalidCron, fmt.Sprintf("parse cron %q: %v", triggers.Cron, err))
			test.Status.NextRun = nil
		}
	}

	if schedule != nil {
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
	backups := make(map[string]restorev1alpha1.HandledBackup, len(test.Spec.Sources))
	used := make([]restorev1alpha1.HandledBackup, 0, len(test.Spec.Sources))
	names := make([]string, 0, len(test.Spec.Sources))

	var (
		due     time.Time
		waiting []string
	)

	for _, src := range test.Spec.Sources {
		list, err := r.completedBackups(ctx, test, src.BackupSelector)
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

		newest := list[len(list)-1].handled(src.SourcePVC)
		backups[src.SourcePVC] = newest
		names = append(names, newest.Name)
		used = append(used, newest)
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

	test.Status.PendingBackups = pendingList(backups)
	test.Status.PendingRunSource = "queued-" + trig.source

	trig.consume()

	return true, nil
}

// replace deletes the run in progress and starts the trigger in its place.
// The backups are resolved first, so a run that cannot start does not cost the run in progress.
func (r *RestoreTestReconciler) replace(ctx context.Context, test *restorev1alpha1.RestoreTest, trig *trigger) (bool, error) {
	backups, ok, err := r.resolveOrBlock(ctx, test, trig.backups)
	if err != nil || !ok {
		return false, err
	}

	old := &restorev1alpha1.Restore{}

	err = r.Get(ctx, client.ObjectKey{Namespace: test.Namespace, Name: test.Status.CurrentRestore}, old)
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

	if err = r.createRestore(ctx, test, startRunID(test, trig.source), backups, 1); err != nil {
		return false, err
	}

	trig.consume()

	return true, nil
}

// start creates the first attempt of a new run. Sources without a preferred backup take their newest one.
func (r *RestoreTestReconciler) start(
	ctx context.Context,
	test *restorev1alpha1.RestoreTest,
	source string,
	preferred map[string]restorev1alpha1.HandledBackup,
) (bool, error) {
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
	preferred map[string]restorev1alpha1.HandledBackup,
) (map[string]restorev1alpha1.HandledBackup, bool, error) {
	backups, missing, err := r.resolveBackups(ctx, test, preferred)
	if err != nil {
		return nil, false, err
	}

	if len(missing) > 0 {
		r.blockTest(test, reasonBackupNotFound, "no finished backup matches backupSelector for source(s) "+strings.Join(missing, ", "))

		return nil, false, nil
	}

	return backups, true, nil
}

// createRestore creates the Restore of one attempt and makes it the run in progress.
// An existing attempt is accepted. It can come from an earlier reconcile that did not store its status.
// VolSync sources are pinned to the completion time of their backup, so a later sync does not change the run.
func (r *RestoreTestReconciler) createRestore(
	ctx context.Context,
	test *restorev1alpha1.RestoreTest,
	run string,
	backups map[string]restorev1alpha1.HandledBackup,
	attempt int,
) error {
	sources := make([]restorev1alpha1.RestoreSource, 0, len(test.Spec.Sources))
	for _, src := range test.Spec.Sources {
		b := backups[src.SourcePVC]
		rs := restorev1alpha1.RestoreSource{
			SourcePVC:     src.SourcePVC,
			BackupRef:     restorev1alpha1.BackupRef{Name: b.Name},
			RestoreTarget: src.RestoreTarget,
		}

		if test.Spec.Provider == restorev1alpha1.ProviderVolSync && !b.CreatedAt.IsZero() {
			at := b.CreatedAt
			rs.RestoreAsOf = &at
		}

		sources = append(sources, rs)
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
			Velero:          test.Spec.Velero.DeepCopy(),
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
	if apierrors.IsAlreadyExists(err) {
		err = r.ownAttempt(ctx, test, restore.Name)
	}

	if err != nil {
		return err
	}

	test.Status.CurrentRestore = restore.Name
	test.Status.RetryAt = nil

	setTestReady(test, metav1.ConditionFalse, reasonRunning, "attempt "+strconv.Itoa(attempt)+" started")

	return nil
}

// errRestoreBusy means the Restore name of the next attempt is taken by an object this run cannot use.
var errRestoreBusy = errors.New("restore name is taken by a Restore that is being deleted or not owned by this RestoreTest")

// ownAttempt accepts an existing Restore as this attempt only when this RestoreTest controls it and it is not
// being deleted. A Restore of a deleted RestoreTest, or one a replaced run is still cleaning up, would report a
// result this run never produced.
func (r *RestoreTestReconciler) ownAttempt(ctx context.Context, test *restorev1alpha1.RestoreTest, name string) error {
	existing := &restorev1alpha1.Restore{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: test.Namespace, Name: name}, existing); err != nil {
		return err
	}

	owner := metav1.GetControllerOf(existing)
	if owner == nil || owner.UID != test.UID || !existing.DeletionTimestamp.IsZero() {
		return fmt.Errorf("%w: %s", errRestoreBusy, name)
	}

	return nil
}

// resolveBackups picks one backup per source: preferred, else the newest finished one that matches the selector.
// Sources without a backup are returned in missing.
func (r *RestoreTestReconciler) resolveBackups(
	ctx context.Context,
	test *restorev1alpha1.RestoreTest,
	preferred map[string]restorev1alpha1.HandledBackup,
) (map[string]restorev1alpha1.HandledBackup, []string, error) {
	backups := make(map[string]restorev1alpha1.HandledBackup, len(test.Spec.Sources))

	var missing []string

	for _, src := range test.Spec.Sources {
		if b, ok := preferred[src.SourcePVC]; ok && b.Name != "" {
			backups[src.SourcePVC] = b

			continue
		}

		list, err := r.completedBackups(ctx, test, src.BackupSelector)
		if err != nil {
			return nil, nil, err
		}

		if len(list) == 0 {
			missing = append(missing, src.SourcePVC)

			continue
		}

		backups[src.SourcePVC] = list[len(list)-1].handled(src.SourcePVC)
	}

	return backups, missing, nil
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

// pendingList stores picked backups in the status shape, sorted by sourcePVC.
func pendingList(backups map[string]restorev1alpha1.HandledBackup) []restorev1alpha1.HandledBackup {
	list := make([]restorev1alpha1.HandledBackup, 0, len(backups))
	for _, b := range backups {
		list = append(list, b)
	}

	sort.Slice(list, func(i, j int) bool { return list[i].SourcePVC < list[j].SourcePVC })

	return list
}

// pendingMap reads picked backups from the status shape.
func pendingMap(list []restorev1alpha1.HandledBackup) map[string]restorev1alpha1.HandledBackup {
	backups := make(map[string]restorev1alpha1.HandledBackup, len(list))
	for _, b := range list {
		backups[b.SourcePVC] = b
	}

	return backups
}
