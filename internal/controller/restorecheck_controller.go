package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	restorev1alpha1 "github.com/linogics/bret-operator/api/v1alpha1"
)

// RestoreCheckReconciler reconciles a RestoreCheck object. It validates the spec and reports the result.
// The check pods are built by the Restore controller, not here.
type RestoreCheckReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=bret.linogics.io,resources=restorechecks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bret.linogics.io,resources=restorechecks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=bret.linogics.io,resources=restorechecks/finalizers,verbs=update

// Reconcile validates the RestoreCheck spec and stores a Ready condition with the result.
func (r *RestoreCheckReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	check := &restorev1alpha1.RestoreCheck{}
	if err := r.Get(ctx, req.NamespacedName, check); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	cond := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "Valid",
		Message:            "check spec is valid",
		ObservedGeneration: check.Generation,
	}

	if err := validateCheck(check); err != nil {
		cond.Status = metav1.ConditionFalse
		cond.Reason = "Invalid"
		cond.Message = err.Error()
	}

	meta.SetStatusCondition(&check.Status.Conditions, cond)
	check.Status.ObservedGeneration = check.Generation

	return ctrl.Result{}, r.Status().Update(ctx, check)
}

// SetupWithManager sets up the controller with the Manager.
func (r *RestoreCheckReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&restorev1alpha1.RestoreCheck{}).
		Named("restorecheck").
		Complete(r)
}
