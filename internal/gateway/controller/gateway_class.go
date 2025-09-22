package controller

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

type GatewayClassReconciler struct {
	client.Client
	ControllerName string
}

// Reconcile for gateway class ensures that class is managed by our controller and update gateway class status.
func (r *GatewayClassReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	var gc gatewayv1.GatewayClass
	if err := r.Get(ctx, req.NamespacedName, &gc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if string(gc.Spec.ControllerName) != r.ControllerName {
		log.V(1).Info("ControllerName does not match, skipping", "found", gc.Spec.ControllerName, "expected", r.ControllerName)
		return ctrl.Result{}, nil
	}

	newCondition := metav1.Condition{
		Type:    string(gatewayv1.GatewayClassConditionStatusAccepted),
		Status:  metav1.ConditionTrue,
		Reason:  "Accepted",
		Message: "GatewayClass accepted by controller",
	}
	meta.SetStatusCondition(&gc.Status.Conditions, newCondition)

	if err := r.Status().Update(ctx, &gc); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update GatewayClass status: %w", err)
	}
	log.V(1).Info("Set Accepted=True for GatewayClass", "name", gc.Name)
	return ctrl.Result{}, nil
}

func (r *GatewayClassReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1.GatewayClass{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}
