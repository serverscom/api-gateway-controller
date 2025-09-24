package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/cache"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// findGatewaysForHTTPRoute returns reconcile requests with gateways that affected by changes in httpRoute
func (r *GatewayReconciler) findGatewaysForHTTPRoute(ctx context.Context, obj client.Object) []reconcile.Request {
	route := obj.(*gatewayv1.HTTPRoute)
	var requests []reconcile.Request

	parentKeys := r.getParentGatewayKeys(route)
	for _, key := range parentKeys {
		namespace, name, err := cache.SplitMetaNamespaceKey(key)
		if err != nil {
			continue
		}

		var gw gatewayv1.Gateway
		if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &gw); err != nil {
			if !apierrors.IsNotFound(err) {
				ctrl.LoggerFrom(ctx).V(1).Info("HTTPRoute parent gateway not found", "route", route.Name, "gateway", key, "error", err)
			}
			continue
		}

		managed, err := r.isManagedGateway(ctx, &gw)
		if err != nil {
			ctrl.LoggerFrom(ctx).V(1).Info("Failed to check if gateway is managed", "route", route.Name, "gateway", key, "error", err)
			continue
		}
		if !managed {
			ctrl.LoggerFrom(ctx).V(1).Info("HTTPRoute parent gateway not managed", "route", route.Name, "gateway", key)
			continue
		}

		ctrl.LoggerFrom(ctx).V(3).Info("HTTPRoute change triggers Gateway reconcile", "route", route.Name, "gateway", key)
		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKey{Namespace: namespace, Name: name},
		})
	}

	return requests
}

// findGatewaysForService returns reconcile requests with gateways that affected by changes in Service
func (r *GatewayReconciler) findGatewaysForService(ctx context.Context, obj client.Object) []reconcile.Request {
	service := obj.(*corev1.Service)
	var requests []reconcile.Request

	var httpRoutes gatewayv1.HTTPRouteList
	if err := r.List(ctx, &httpRoutes); err != nil {
		ctrl.LoggerFrom(ctx).Error(err, "Failed to list HTTPRoutes for service change", "service", service.Name)
		return nil
	}

	processedGateways := make(map[string]bool)

	for _, route := range httpRoutes.Items {
		if !r.routeReferencesService(&route, service) {
			continue
		}

		parentKeys := r.getParentGatewayKeys(&route)
		for _, parent := range parentKeys {
			if processedGateways[parent] {
				continue
			}

			namespace, name, err := cache.SplitMetaNamespaceKey(parent)
			if err != nil {
				continue
			}

			var gw gatewayv1.Gateway
			if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &gw); err != nil {
				if !apierrors.IsNotFound(err) {
					ctrl.LoggerFrom(ctx).V(1).Info("Service parent gateway not found", "service", service.Name, "gateway", parent, "error", err)
				}
				continue
			}

			managed, err := r.isManagedGateway(ctx, &gw)
			if err != nil {
				ctrl.LoggerFrom(ctx).V(1).Info("Failed to check if gateway is managed", "service", service.Name, "gateway", parent, "error", err)
				continue
			}
			if !managed {
				ctrl.LoggerFrom(ctx).V(1).Info("Service parent gateway not managed", "service", service.Name, "gateway", parent)
				continue
			}

			ctrl.LoggerFrom(ctx).V(3).Info("Service change triggers Gateway reconcile", "service", service.Name, "gateway", parent)
			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKey{Namespace: namespace, Name: name},
			})
			processedGateways[parent] = true
		}
	}

	return requests
}

// findGatewaysForService returns reconcile requests with gateways that affected by changes in Secret
func (r *GatewayReconciler) findGatewaysForSecret(ctx context.Context, obj client.Object) []reconcile.Request {
	secret := obj.(*corev1.Secret)
	var requests []reconcile.Request

	var gateways gatewayv1.GatewayList
	if err := r.List(ctx, &gateways, client.InNamespace(secret.Namespace)); err != nil {
		ctrl.LoggerFrom(ctx).Error(err, "Failed to list Gateways for secret change", "secret", secret.Name)
		return nil
	}

	for _, gw := range gateways.Items {
		if !r.gatewayReferencesSecret(&gw, secret) {
			continue
		}

		managed, err := r.isManagedGateway(ctx, &gw)
		if err != nil {
			ctrl.LoggerFrom(ctx).V(1).Info("Failed to check if gateway is managed", "secret", secret.Name, "gateway", gw.Name, "error", err)
			continue
		}
		if !managed {
			ctrl.LoggerFrom(ctx).V(1).Info("Secret gateway not managed", "secret", secret.Name, "gateway", gw.Name)
			continue
		}

		ctrl.LoggerFrom(ctx).V(1).Info("Secret change triggers Gateway reconcile", "secret", secret.Name, "gateway", gw.Name)
		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKey{Namespace: gw.Namespace, Name: gw.Name},
		})
	}

	return requests
}

// getParentGatewayKeys returns gateways for HTTPRoute
func (r *GatewayReconciler) getParentGatewayKeys(route *gatewayv1.HTTPRoute) []string {
	var keys []string
	for _, parent := range route.Spec.ParentRefs {
		if parent.Kind != nil && string(*parent.Kind) != "Gateway" {
			continue
		}
		if parent.Group != nil && *parent.Group != gatewayv1.GroupName {
			continue
		}
		ns := route.Namespace
		if parent.Namespace != nil {
			ns = string(*parent.Namespace)
		}
		name := string(parent.Name)
		keys = append(keys, ns+"/"+name)
	}
	return keys
}

// routeReferencesService returns true if the given HTTPRoute references the specified Service.
func (r *GatewayReconciler) routeReferencesService(route *gatewayv1.HTTPRoute, service *corev1.Service) bool {
	for _, rule := range route.Spec.Rules {
		for _, backendRef := range rule.BackendRefs {
			// skip not core Services
			if backendRef.BackendObjectReference.Group != nil && *backendRef.BackendObjectReference.Group != "" {
				continue
			}
			if string(backendRef.BackendObjectReference.Name) != service.Name {
				continue
			}

			ns := route.Namespace
			if backendRef.BackendObjectReference.Namespace != nil {
				ns = string(*backendRef.BackendObjectReference.Namespace)
			}
			if ns == service.Namespace {
				return true
			}
		}
	}
	return false
}

// gatewayReferencesSecret returns true if the given Gateway references the specified Secret.
func (r *GatewayReconciler) gatewayReferencesSecret(gw *gatewayv1.Gateway, secret *corev1.Secret) bool {
	for _, listener := range gw.Spec.Listeners {
		if listener.TLS == nil {
			continue
		}
		for _, certRef := range listener.TLS.CertificateRefs {
			if string(certRef.Name) == secret.Name {
				ns := gw.Namespace
				if certRef.Namespace != nil {
					ns = string(*certRef.Namespace)
				}
				if ns != secret.Namespace {
					continue
				}
				return true
			}
		}
	}
	return false
}
