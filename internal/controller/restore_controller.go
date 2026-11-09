package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

const (
	apiVersionAlpha1     = "v1alpha1"
	fieldKind            = "kind"
	readyConditionType   = "Ready"
	restoreRequeueAfter  = 10 * time.Second
	checkRequeueAfter    = 30 * time.Second
	defaultRestoreWindow = 45 * time.Minute
)

// Event and condition reasons for Restore.
const (
	reasonRestoreStarted      = "RestoreStarted"
	reasonRestoreSucceeded    = "RestoreSucceeded"
	reasonRestoreFailed       = "RestoreFailed"
	reasonRestoreCanceled     = "RestoreCanceled"
	reasonCheckPassed         = "CheckPassed"
	reasonCheckFailed         = "CheckFailed"
	reasonProviderUnsupported = "ProviderUnsupported"
	reasonCheckNotFound       = "CheckNotFound"
	reasonCheckUnsupported    = "CheckUnsupported"
	reasonRepositoryMissing   = "RepositoryMissing"
)

// cleanupFinalizer makes the Restore remove its provider objects, scratch namespace and PVCs on deletion.
// Owner references cannot reach the objects outside the Restore namespace.
const cleanupFinalizer = "bret.linogics.io/cleanup"

// RestoreReconciler reconciles a Restore object. One attempt restores with Kopiur and runs the check.
type RestoreReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	// APIReader reads provider objects. It starts no informer for them. Nil falls back to the client.
	APIReader client.Reader
	// EnabledProviders limits the providers this reconciler serves. Nil serves all of them.
	EnabledProviders map[restorev1alpha1.Provider]bool
	// VeleroNamespace is the Velero install namespace. Empty means "velero".
	VeleroNamespace string
}

// +kubebuilder:rbac:groups=bret.linogics.io,resources=restores,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bret.linogics.io,resources=restores/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=bret.linogics.io,resources=restores/finalizers,verbs=update
// +kubebuilder:rbac:groups=bret.linogics.io,resources=restorechecks,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=pods;persistentvolumeclaims,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;patch;delete
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=kopiur.home-operations.com,resources=restores,verbs=get;list;watch;create;update;patch;delete

// Reconcile runs one step of the Restore state machine and stores the status it produced.
// Terminal restores only have their objects cleaned up.
func (r *RestoreReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	restore := &restorev1alpha1.Restore{}
	if err := r.Get(ctx, req.NamespacedName, restore); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !restore.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.finalize(ctx, restore)
	}

	if controllerutil.AddFinalizer(restore, cleanupFinalizer) {
		if err := r.Update(ctx, restore); err != nil {
			return ctrl.Result{}, err
		}
	}

	var (
		result ctrl.Result
		err    error
	)

	if !isTerminal(restore.Status.Phase) {
		result, err = r.advance(ctx, restore)
	}

	if statusErr := r.Status().Update(ctx, restore); statusErr != nil {
		return ctrl.Result{}, errors.Join(err, statusErr)
	}

	// Cleanup runs after the terminal status is stored, so a failed cleanup is retried
	// without recreating the objects of the attempt.
	if isTerminal(restore.Status.Phase) {
		if cleanupErr := r.cleanup(ctx, restore); cleanupErr != nil {
			return ctrl.Result{}, errors.Join(err, cleanupErr)
		}
	}

	return result, err
}

// finalize removes everything the attempt created, then releases the Restore.
func (r *RestoreReconciler) finalize(ctx context.Context, restore *restorev1alpha1.Restore) error {
	if !controllerutil.ContainsFinalizer(restore, cleanupFinalizer) {
		return nil
	}

	if err := r.cleanup(ctx, restore); err != nil {
		return err
	}

	controllerutil.RemoveFinalizer(restore, cleanupFinalizer)

	return client.IgnoreNotFound(r.Update(ctx, restore))
}

// advance runs one step of the attempt and reports how the reconciler should be requeued.
func (r *RestoreReconciler) advance(ctx context.Context, restore *restorev1alpha1.Restore) (ctrl.Result, error) {
	if !r.providerEnabled(restore.Spec.Provider) {
		r.block(restore, reasonProviderUnsupported, fmt.Sprintf("provider %s is not supported yet", restore.Spec.Provider))

		return ctrl.Result{}, nil
	}

	bp := &restorev1alpha1.RestoreCheck{}

	key := client.ObjectKey{Namespace: restore.Namespace, Name: restore.Spec.RestoreCheckRef.Name}
	if err := r.Get(ctx, key, bp); err != nil {
		if apierrors.IsNotFound(err) {
			r.block(restore, reasonCheckNotFound, fmt.Sprintf("RestoreCheck %s not found", key.Name))

			return ctrl.Result{RequeueAfter: checkRequeueAfter}, nil
		}

		return ctrl.Result{}, err
	}

	err := validateRestoredVolumes(bp)
	if err != nil {
		restore.Status.Phase = restorev1alpha1.RestorePhaseFailed
		r.block(restore, reasonProviderUnsupported, err.Error())
		recordRestoreRun(restore, restorev1alpha1.RestorePhaseFailed)

		return ctrl.Result{}, nil
	}

	err = validateCheck(bp)
	if err != nil {
		r.block(restore, reasonCheckUnsupported, err.Error())

		return ctrl.Result{}, nil
	}

	if restore.Spec.Provider == restorev1alpha1.ProviderVelero {
		if err = validateVeleroCheck(bp); err != nil {
			restore.Status.Phase = restorev1alpha1.RestorePhaseFailed
			r.block(restore, reasonProviderUnsupported, err.Error())
			recordRestoreRun(restore, restorev1alpha1.RestorePhaseFailed)

			return ctrl.Result{}, nil
		}
	}

	uid, err := moverUID(bp)
	if err != nil {
		r.block(restore, reasonCheckUnsupported, err.Error())

		return ctrl.Result{}, nil
	}

	if restore.Status.StartTime == nil {
		startAttempt(restore, time.Now())

		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	if restore.Status.Deadline != nil && !time.Now().Before(restore.Status.Deadline.Time) {
		r.finish(restore, restorev1alpha1.RestorePhaseCanceled, reasonRestoreCanceled, "restore window expired")

		return ctrl.Result{}, nil
	}

	return r.progress(ctx, restore, bp, uid)
}

// progress creates the claims and the Kopiur Restore, then runs the check once Kopiur completes.
func (r *RestoreReconciler) progress(
	ctx context.Context,
	restore *restorev1alpha1.Restore,
	bp *restorev1alpha1.RestoreCheck,
	uid *int64,
) (ctrl.Result, error) {
	size, err := r.scratchSize(ctx, restore)
	if err != nil {
		if !errors.Is(err, ErrSizeUnknown) && !errors.Is(err, ErrInsufficientCapacity) {
			return ctrl.Result{}, err
		}

		// Terminal for this attempt: nothing is created and nothing is requeued.
		reason := restorev1alpha1.ReasonSizeUnknown
		if errors.Is(err, ErrInsufficientCapacity) {
			reason = restorev1alpha1.ReasonInsufficientCapacity
		}

		restore.Status.Phase = restorev1alpha1.RestorePhaseFailed
		r.block(restore, reason, err.Error())
		recordRestoreRun(restore, restorev1alpha1.RestorePhaseFailed)

		return ctrl.Result{}, nil
	}

	if restore.Spec.Provider == restorev1alpha1.ProviderVelero {
		return r.reconcileVelero(ctx, restore, bp)
	}

	if restore.Spec.Provider == restorev1alpha1.ProviderVolSync {
		return r.reconcileVolSync(ctx, restore, bp, size, uid)
	}

	claims, err := r.ensureClaims(ctx, restore, bp, size)
	if err != nil {
		return ctrl.Result{}, err
	}

	kopiur := kopiurRestoreObject(restore, uid)

	created, err := r.ensure(ctx, restore, kopiur)
	if err != nil {
		return ctrl.Result{}, err
	}

	if created {
		r.Recorder.Event(restore, corev1.EventTypeNormal, reasonRestoreStarted, "started Kopiur restore "+kopiur.GetName())
	}

	restore.Status.ProviderRef = &restorev1alpha1.ProviderRef{
		Kind:      kopiurRestoreGVK.Kind,
		Name:      kopiur.GetName(),
		Namespace: kopiur.GetNamespace(),
	}
	setRunning(restore)

	switch kopiurPhase(kopiur) {
	case kopiurPhaseFailed:
		r.finish(restore, restorev1alpha1.RestorePhaseFailed, reasonRestoreFailed, "Kopiur restore failed")

		return ctrl.Result{}, nil
	case kopiurPhaseCompleted:
		return r.check(ctx, restore, bp, restoredClaimName(restore), restore.Namespace, claims)
	default:
		return ctrl.Result{RequeueAfter: restoreRequeueAfter}, nil
	}
}

// scratchSize resolves the scratch size once and stores it in the status.
// Later reconciles reuse it, so the source is read once.
func (r *RestoreReconciler) scratchSize(ctx context.Context, restore *restorev1alpha1.Restore) (resource.Quantity, error) {
	if restore.Status.ResolvedSize != nil {
		return *restore.Status.ResolvedSize, nil
	}

	// A Restore from before the size was stored has no size to create objects with.
	if restore.Status.ProviderRef != nil {
		return resource.Quantity{}, ErrSizeUnknown
	}

	source, err := r.sourceSize(ctx, restore)
	if err != nil {
		return resource.Quantity{}, err
	}

	size, err := resolveSize(restore.Spec.Target, source)
	if err != nil {
		return resource.Quantity{}, err
	}

	restore.Status.ResolvedSize = &size

	return size, nil
}

// ensureClaims creates the restored PVC and the check scratch PVCs. It returns the scratch claims by volume name.
func (r *RestoreReconciler) ensureClaims(
	ctx context.Context,
	restore *restorev1alpha1.Restore,
	bp *restorev1alpha1.RestoreCheck,
	size resource.Quantity,
) (map[string]string, error) {
	if _, err := r.ensure(ctx, restore, restoredPVC(restore, size)); err != nil {
		return nil, err
	}

	return r.ensureScratchClaims(ctx, restore, bp)
}

// ensureScratchClaims creates the check scratch PVCs. It returns them by volume name.
func (r *RestoreReconciler) ensureScratchClaims(
	ctx context.Context,
	restore *restorev1alpha1.Restore,
	bp *restorev1alpha1.RestoreCheck,
) (map[string]string, error) {
	claims := make(map[string]string, len(bp.Spec.ScratchVolumes))

	for _, sv := range bp.Spec.ScratchVolumes {
		pvc := scratchPVC(restore, sv)

		if _, err := r.ensure(ctx, restore, pvc); err != nil {
			return nil, err
		}

		claims[sv.Name] = pvc.Name
	}

	return claims, nil
}

// check creates the check pod and maps its result to the terminal phase.
func (r *RestoreReconciler) check(
	ctx context.Context,
	restore *restorev1alpha1.Restore,
	bp *restorev1alpha1.RestoreCheck,
	restoredClaim, namespace string,
	claims map[string]string,
) (ctrl.Result, error) {
	pod, err := buildCheckPod(restore, bp, restoredClaim, claims)
	if err != nil {
		r.block(restore, reasonCheckUnsupported, err.Error())

		return ctrl.Result{}, nil
	}

	pod.Namespace = namespace

	if _, err := r.ensure(ctx, restore, pod); err != nil {
		return ctrl.Result{}, err
	}

	switch pod.Status.Phase {
	case corev1.PodSucceeded:
		r.Recorder.Event(restore, corev1.EventTypeNormal, reasonCheckPassed, "check pod "+pod.Name+" passed")
		r.finish(restore, restorev1alpha1.RestorePhaseSucceeded, reasonRestoreSucceeded, "restore and check passed")

		return ctrl.Result{}, nil
	case corev1.PodFailed:
		r.Recorder.Event(restore, corev1.EventTypeWarning, reasonCheckFailed, "check pod "+pod.Name+" failed")
		r.finish(restore, restorev1alpha1.RestorePhaseFailed, reasonRestoreFailed, "check pod failed")

		return ctrl.Result{}, nil
	default:
		return ctrl.Result{RequeueAfter: restoreRequeueAfter}, nil
	}
}

// startAttempt sets the first attempt to Pending with its window.
func startAttempt(restore *restorev1alpha1.Restore, now time.Time) {
	window := restore.Spec.RestoreWindow.Duration
	if window == 0 {
		window = defaultRestoreWindow
	}

	start := metav1.NewTime(now)
	deadline := metav1.NewTime(now.Add(window))

	restore.Status.Phase = restorev1alpha1.RestorePhasePending
	restore.Status.Attempt = 1
	restore.Status.StartTime = &start
	restore.Status.Deadline = &deadline
	setReady(restore, metav1.ConditionFalse, string(restorev1alpha1.RestorePhasePending), "restore is pending")
}

func setRunning(restore *restorev1alpha1.Restore) {
	restore.Status.Phase = restorev1alpha1.RestorePhaseRunning
	setReady(restore, metav1.ConditionFalse, string(restorev1alpha1.RestorePhaseRunning), "restore is in progress")
}

// finish moves the attempt into a terminal phase and records the event and metrics.
func (r *RestoreReconciler) finish(
	restore *restorev1alpha1.Restore,
	phase restorev1alpha1.RestorePhase,
	reason, msg string,
) {
	restore.Status.Phase = phase

	eventType := corev1.EventTypeWarning
	status := metav1.ConditionFalse

	if phase == restorev1alpha1.RestorePhaseSucceeded {
		eventType = corev1.EventTypeNormal
		status = metav1.ConditionTrue
	}

	setReady(restore, status, string(phase), msg)
	r.Recorder.Event(restore, eventType, reason, msg)
	recordRestoreRun(restore, phase)
}

// block sets Ready=False with a reason and emits a warning event when the reason changes.
func (r *RestoreReconciler) block(restore *restorev1alpha1.Restore, reason, msg string) {
	if readyReason(restore) != reason {
		r.Recorder.Event(restore, corev1.EventTypeWarning, reason, msg)
	}

	setReady(restore, metav1.ConditionFalse, reason, msg)
}

func setReady(restore *restorev1alpha1.Restore, status metav1.ConditionStatus, reason, msg string) {
	restore.Status.Message = msg

	meta.SetStatusCondition(&restore.Status.Conditions, metav1.Condition{
		Type:               readyConditionType,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: restore.Generation,
	})
}

func readyReason(restore *restorev1alpha1.Restore) string {
	condition := meta.FindStatusCondition(restore.Status.Conditions, readyConditionType)
	if condition == nil {
		return ""
	}

	return condition.Reason
}

func isTerminal(phase restorev1alpha1.RestorePhase) bool {
	switch phase {
	case restorev1alpha1.RestorePhaseSucceeded, restorev1alpha1.RestorePhaseFailed, restorev1alpha1.RestorePhaseCanceled:
		return true
	default:
		return false
	}
}

// providerEnabled reports whether the provider is known and switched on.
func (r *RestoreReconciler) providerEnabled(p restorev1alpha1.Provider) bool {
	switch p {
	case restorev1alpha1.ProviderKopiur, restorev1alpha1.ProviderVelero, restorev1alpha1.ProviderVolSync:
		return r.EnabledProviders == nil || r.EnabledProviders[p]
	}

	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *RestoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&restorev1alpha1.Restore{}).
		Named("restore").
		Complete(r)
}
