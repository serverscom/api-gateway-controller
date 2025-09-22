package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/serverscom/api-gateway-controller/internal/config"
	lbsrv "github.com/serverscom/api-gateway-controller/internal/service/lb"
	tlssrv "github.com/serverscom/api-gateway-controller/internal/service/tls"
	"github.com/serverscom/api-gateway-controller/internal/types"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

var (
	IPAddressType = gatewayv1.IPAddressType
)

// GatewayReconciler reconciles a Gateway object
type GatewayReconciler struct {
	client.Client    // controller-runtime client
	Recorder         record.EventRecorder
	ControllerName   string
	GatewayClassName string

	LBMgr  lbsrv.LBManagerInterface
	TLSMgr tlssrv.TLSManagerInterface
}

// SetupWithManager sets up controller with Manager
func (r *GatewayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(
			&gatewayv1.Gateway{},
			builder.WithPredicates(r.managedPredicate()),
		).
		Watches(
			&gatewayv1.HTTPRoute{},
			handler.EnqueueRequestsFromMapFunc(r.findGatewaysForHTTPRoute),
		).
		Watches(
			&corev1.Service{},
			handler.EnqueueRequestsFromMapFunc(r.findGatewaysForService),
		).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.findGatewaysForSecret),
		).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 1,
		}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}

// Reconcile syncs Gateway state with external resources.
// It manages finalizers, TLS, load balancer, and status updates.
func (r *GatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var gw gatewayv1.Gateway
	if err := r.Get(ctx, req.NamespacedName, &gw); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// cleanup if gw was deleted
	if !gw.DeletionTimestamp.IsZero() && controllerutil.ContainsFinalizer(&gw, config.GW_FINALIZER) {
		if err := r.cleanup(ctx, &gw, config.GW_FINALIZER); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	managed, err := r.isManagedGateway(ctx, &gw)
	if err != nil {
		return ctrl.Result{}, err
	}

	// cleanup and update Programmed cond if not managed but was before
	if !managed {
		if err := r.cleanup(ctx, &gw, config.GW_FINALIZER); err != nil {
			return ctrl.Result{}, err
		}

		orig := gw.DeepCopy()
		cond := metav1.Condition{
			Type:               "Programmed",
			Status:             metav1.ConditionFalse,
			Reason:             "NoLongerManaged",
			Message:            fmt.Sprintf("Gateway is no longer managed by %s", r.ControllerName),
			ObservedGeneration: gw.Generation,
		}
		meta.SetStatusCondition(&gw.Status.Conditions, cond)
		if err := r.Status().Patch(ctx, &gw, client.MergeFrom(orig)); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	// add finalizer
	if !controllerutil.ContainsFinalizer(&gw, config.GW_FINALIZER) {
		orig := gw.DeepCopy()
		controllerutil.AddFinalizer(&gw, config.GW_FINALIZER)
		if err := r.Patch(ctx, &gw, client.MergeFrom(orig)); err != nil {
			return ctrl.Result{}, err
		}
	}

	tlsInfo, err := r.buildTLSInfo(ctx, &gw)
	if err != nil {
		r.Recorder.Event(&gw, corev1.EventTypeWarning, "InvalidTLS", err.Error())
		_ = r.setGatewayStatusCondition(ctx, &gw, "Accepted", "InvalidTLS", err.Error(), metav1.ConditionFalse)
		return ctrl.Result{}, nil

	}

	gwInfo, err := r.buildGatewayInfo(ctx, &gw)
	if err != nil {
		r.Recorder.Event(&gw, corev1.EventTypeWarning, "InvalidGateway", err.Error())
		_ = r.setGatewayStatusCondition(ctx, &gw, "Accepted", "InvalidGateway", err.Error(), metav1.ConditionFalse)
		return ctrl.Result{}, nil
	}

	// set Accepted cond
	_ = r.setGatewayStatusCondition(ctx, &gw, "Accepted", "Accepted", "Gateway is valid and accepted", metav1.ConditionTrue)

	// sync tls
	hostsCertIDMap, err := r.TLSMgr.EnsureTLS(ctx, tlsInfo)
	if err != nil {
		_ = r.setGatewayStatusCondition(ctx, &gw, "Programmed", "SyncTLSFailed", err.Error(), metav1.ConditionFalse)
		r.Recorder.Event(&gw, corev1.EventTypeWarning, "SyncTLSFailed", err.Error())
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// sync lb
	lb, err := r.LBMgr.EnsureLB(ctx, gwInfo, hostsCertIDMap)
	if err != nil {
		_ = r.setGatewayStatusCondition(ctx, &gw, "Programmed", "SyncFailed", err.Error(), metav1.ConditionFalse)
		r.Recorder.Event(&gw, corev1.EventTypeWarning, "SyncFailed", err.Error())
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil

	}

	if strings.ToLower(lb.Status) != config.LB_ACTIVE_STATUS {
		msg := "Load balancer created, waiting for status=Active"
		_ = r.setGatewayStatusCondition(ctx, &gw, "Programmed", "Created", msg, metav1.ConditionFalse)
		r.Recorder.Event(&gw, corev1.EventTypeWarning, "Created", msg)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	var addresses []gatewayv1.GatewayStatusAddress
	for _, ip := range lb.ExternalAddresses {
		addresses = append(addresses, gatewayv1.GatewayStatusAddress{Type: &IPAddressType, Value: ip})
	}

	// not use SetGatewayStatusCondition because we need update addresses too
	orig := gw.DeepCopy()
	gw.Status.Addresses = addresses
	cond := metav1.Condition{
		Type:               "Programmed",
		Status:             metav1.ConditionTrue,
		Reason:             "Programmed",
		Message:            "Successfully programmed",
		ObservedGeneration: gw.Generation,
	}
	meta.SetStatusCondition(&gw.Status.Conditions, cond)
	if err := r.Status().Patch(ctx, &gw, client.MergeFrom(orig)); err != nil {
		return ctrl.Result{}, err
	}
	r.Recorder.Event(&gw, corev1.EventTypeNormal, "Synced", "Successfully synced")

	return ctrl.Result{}, nil
}

// isManagedGateway checks if gateway has our controller name and class
func (r *GatewayReconciler) isManagedGateway(ctx context.Context, gw *gatewayv1.Gateway) (bool, error) {
	var gwClass gatewayv1.GatewayClass
	gwClassName := string(gw.Spec.GatewayClassName)

	if err := r.Get(ctx, client.ObjectKey{Name: gwClassName}, &gwClass); err != nil {
		return false, client.IgnoreNotFound(err)
	}

	if string(gwClass.Spec.ControllerName) != r.ControllerName {
		return false, nil
	}

	if r.GatewayClassName != "" && gwClass.Name != r.GatewayClassName {
		return false, nil
	}

	return true, nil
}

// managedPredicate filters not managed gateways before reconcile loop
func (r *GatewayReconciler) managedPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			gw := e.Object.(*gatewayv1.Gateway)
			managed, _ := r.isManagedGateway(context.Background(), gw)
			return managed
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldGw := e.ObjectOld.(*gatewayv1.Gateway)
			newGw := e.ObjectNew.(*gatewayv1.Gateway)
			if !newGw.DeletionTimestamp.IsZero() {
				return true // handle orphaned gateways
			}
			oldManaged, _ := r.isManagedGateway(context.Background(), oldGw)
			newManaged, _ := r.isManagedGateway(context.Background(), newGw)
			return oldManaged || newManaged
		},
		GenericFunc: func(e event.GenericEvent) bool { return false },
	}
}

// cleanup ensures that load balancer deleted and removes finalizer
func (r *GatewayReconciler) cleanup(ctx context.Context, gw *gatewayv1.Gateway, finalizer string) error {
	labelSelector := config.GW_LABEL_ID + "=" + string(gw.UID)
	if err := r.LBMgr.DeleteLB(ctx, labelSelector); err != nil {
		return err
	}

	orig := gw.DeepCopy()
	controllerutil.RemoveFinalizer(gw, finalizer)
	if err := r.Patch(ctx, gw, client.MergeFrom(orig)); err != nil {
		return err
	}

	return nil
}

// buildGatewayInfo gathers all info needed to build load balancer input.
func (r *GatewayReconciler) buildGatewayInfo(ctx context.Context, gw *gatewayv1.Gateway) (*types.GatewayInfo, error) {
	log := ctrl.LoggerFrom(ctx)
	nodeIps, err := r.getNodesIpList(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get nodes IPs: %w", err)
	}

	// prepare listeners
	seenListeners := make(map[gatewayv1.SectionName]bool)
	var listeners []types.ListenerInfo

	for _, l := range gw.Spec.Listeners {
		if seenListeners[l.Name] {
			return nil, fmt.Errorf("duplicate listener name: %q", l.Name)
		}
		seenListeners[l.Name] = true
		var hostname string
		if l.Hostname != nil {
			hostname = string(*l.Hostname)
		}
		// allowedRoutes
		allowedFrom := "Same" // default
		selector := map[string]string(nil)

		if l.AllowedRoutes != nil && l.AllowedRoutes.Namespaces != nil {
			ns := l.AllowedRoutes.Namespaces
			if ns.From != nil {
				allowedFrom = string(*ns.From)
				if *ns.From == gatewayv1.NamespacesFromSelector && ns.Selector != nil && ns.Selector.MatchLabels != nil {
					selector = ns.Selector.MatchLabels
				}
			}
		}
		listeners = append(listeners, types.ListenerInfo{
			Name:        string(l.Name),
			Hostname:    hostname,
			Protocol:    string(l.Protocol),
			Port:        int32(l.Port),
			AllowedFrom: allowedFrom,
			Selector:    selector,
		})
	}

	vhostMap := map[string]*types.VHostInfo{}
	routeForDomain := map[string]string{}

	var httpRoutes gatewayv1.HTTPRouteList
	if err := r.List(ctx, &httpRoutes); err != nil {
		return nil, fmt.Errorf("failed to list HTTPRoutes: %w", err)
	}

	for _, route := range httpRoutes.Items {
		if !isRouteAttachedToGateway(&route, gw) {
			continue
		}
		var routeHostnames []string
		if len(route.Spec.Hostnames) == 0 {
			return nil, fmt.Errorf("HTTPRoute %s/%s: Hostname must be specified (no wildcards, no empty values supported)", route.Namespace, route.Name)
		}
		for _, h := range route.Spec.Hostnames {
			host := string(h)
			if host == "" || strings.ContainsRune(host, '*') {
				return nil, fmt.Errorf("HTTPRoute %s/%s: Invalid hostname %q (must be concrete, no wildcards, no empty)", route.Namespace, route.Name, host)
			}
			routeHostnames = append(routeHostnames, host)
		}

		sectionNames := map[string]struct{}{}
		for _, pr := range route.Spec.ParentRefs {
			if pr.SectionName != nil {
				sectionNames[string(*pr.SectionName)] = struct{}{}
			}
		}

		nsLabels, err := r.getNamespaceLabels(ctx, route.Namespace)
		if err != nil {
			return nil, fmt.Errorf("cannot get labels for namespace %q: %w", route.Namespace, err)
		}
		for _, hostname := range routeHostnames {
			if prev, ok := routeForDomain[hostname]; ok && prev != route.Name {
				return nil, fmt.Errorf("domain %q used in several HTTPRoute: %q and %q", hostname, prev, route.Name)
			}
			routeForDomain[hostname] = route.Name

			matchedListeners := []types.ListenerInfo{}
			for _, l := range listeners {
				if len(sectionNames) > 0 {
					if _, ok := sectionNames[l.Name]; !ok {
						continue
					}
				}
				if !isRouteNamespaceAllowed(l, gw.Namespace, route.Namespace, nsLabels) {
					continue
				}
				if hostMatches(l.Hostname, hostname) {
					matchedListeners = append(matchedListeners, l)
				}
			}
			if len(matchedListeners) == 0 {
				continue
			}
			// SSL/Ports
			ssl := false
			ports := []int32{}
			for _, l := range matchedListeners {
				if l.Protocol == "HTTPS" {
					ssl = true
				}
			}
			for _, l := range matchedListeners {
				if ssl && l.Protocol == "HTTPS" {
					ports = append(ports, l.Port)
				}
				if !ssl && l.Protocol == "HTTP" {
					ports = append(ports, l.Port)
				}
			}
			vh, exists := vhostMap[hostname]
			if !exists {
				vh = &types.VHostInfo{
					Host:  hostname,
					SSL:   ssl,
					Ports: ports,
				}
				vhostMap[hostname] = vh
			} else {
				existing := map[int32]struct{}{}
				for _, p := range vh.Ports {
					existing[p] = struct{}{}
				}
				for _, p := range ports {
					if _, ok := existing[p]; !ok {
						vh.Ports = append(vh.Ports, p)
					}
				}
				if ssl {
					vh.SSL = true
				}
			}

			// prepare paths
			for _, rule := range route.Spec.Rules {
				if len(rule.BackendRefs) == 0 {
					continue
				}
				if len(rule.Filters) > 0 {
					log.Info("HTTPRoute filters will be ignored", "http_route", route.Namespace+"/"+route.Name, "level", "warn")

				}
				backend := rule.BackendRefs[0]
				if backend.BackendObjectReference.Group != nil && *backend.BackendObjectReference.Group != "" {
					return nil, fmt.Errorf("non-core backend groups not supported: %v", *backend.BackendObjectReference.Group)
				}
				svcName := string(backend.BackendObjectReference.Name)
				ns := route.Namespace
				if backend.BackendObjectReference.Namespace != nil {
					ns = string(*backend.BackendObjectReference.Namespace)
				}
				var svc corev1.Service
				if err := r.Get(ctx, client.ObjectKey{Namespace: ns, Name: svcName}, &svc); err != nil {
					return nil, fmt.Errorf("failed to get service %s/%s: %w", ns, svcName, err)
				}
				var wantPort int32 = 0
				if backend.BackendObjectReference.Port != nil {
					wantPort = int32(*backend.BackendObjectReference.Port)
				} else if len(svc.Spec.Ports) > 0 {
					wantPort = svc.Spec.Ports[0].Port
				}
				var nodePort int32
				found := false
				for _, p := range svc.Spec.Ports {
					if p.Port == wantPort {
						if p.NodePort == 0 {
							return nil, fmt.Errorf("service %s has no NodePort (only NodePort/LoadBalancer supported)", svc.Name)
						}
						nodePort = p.NodePort
						found = true
						break
					}
				}
				if !found {
					return nil, fmt.Errorf("service %s: port %d not found", svc.Name, wantPort)
				}
				paths := []string{}
				if len(rule.Matches) == 0 {
					paths = append(paths, "/")
				} else {
					for _, m := range rule.Matches {
						if m.Path == nil || m.Path.Value == nil {
							continue
						}
						pathType := gatewayv1.PathMatchPathPrefix
						if m.Path.Type != nil {
							pathType = *m.Path.Type
						}
						if pathType != gatewayv1.PathMatchPathPrefix {
							log.Info("unsupported match type in rule, only PathPrefix is supported — skipping", "type", pathType, "http_route", route.Namespace+"/"+route.Name, "level", "warn")
							continue
						}
						paths = append(paths, *m.Path.Value)
					}
				}
				for _, path := range paths {
					vh.Paths = append(vh.Paths, types.PathInfo{
						Path:     path,
						Service:  &svc,
						NodePort: int(nodePort),
						NodeIps:  nodeIps,
					})
				}
			}
		}
	}
	gwInfo := &types.GatewayInfo{
		UID:    string(gw.UID),
		Name:   gw.Name,
		NS:     gw.Namespace,
		VHosts: vhostMap,
	}
	return gwInfo, nil
}

// buildTLSInfo gathers tls info about each domain that can use tls.
func (r *GatewayReconciler) buildTLSInfo(ctx context.Context, gw *gatewayv1.Gateway) (map[string]types.TLSConfigInfo, error) {
	var (
		result = make(map[string]types.TLSConfigInfo)
		errs   []error
	)

	for i, listener := range gw.Spec.Listeners {
		if listener.Protocol != gatewayv1.HTTPSProtocolType {
			continue
		}
		if err := validateHTTPSListener(listener); err != nil {
			errs = append(errs, fmt.Errorf("listener[%d]: %w", i, err))
			continue
		}
		hostname := string(*listener.Hostname)
		if listener.TLS.Options != nil {
			optKey := gatewayv1.AnnotationKey(config.TLS_EXTERNAL_ID_KEY)
			if id, ok := listener.TLS.Options[optKey]; ok && id != "" {
				result[hostname] = types.TLSConfigInfo{
					ExternalID: string(id),
				}
				continue
			}
		}
		var secretName string
		var secretNS = gw.Namespace
		for _, ref := range listener.TLS.CertificateRefs {
			if (ref.Kind == nil || *ref.Kind == "Secret") && (ref.Group == nil || *ref.Group == "") {
				secretName = string(ref.Name)
				break
			}
		}
		if secretName == "" {
			errs = append(errs, fmt.Errorf("listener[%d]: no valid refs found", i))
			continue
		}
		var secret corev1.Secret
		if err := r.Get(ctx, client.ObjectKey{Namespace: secretNS, Name: secretName}, &secret); err != nil {
			return nil, fmt.Errorf("can't get secret %s/%s: %v", secretNS, secretName, err)
		}
		result[hostname] = types.TLSConfigInfo{
			Secret: &secret,
		}
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("validation errors:\n%s", joinErrors(errs))
	}
	return result, nil
}

// getNodesIpList return node ips
func (r *GatewayReconciler) getNodesIpList(ctx context.Context) ([]string, error) {
	var nodes corev1.NodeList
	if err := r.List(ctx, &nodes); err != nil {
		return nil, err
	}

	var nodeIPs []string
	for _, node := range nodes.Items {
		for _, addr := range node.Status.Addresses {
			if addr.Type == corev1.NodeExternalIP || addr.Type == corev1.NodeInternalIP {
				nodeIPs = append(nodeIPs, addr.Address)
				break
			}
		}
	}
	return nodeIPs, nil
}

// getNamespaceLabels return namespace labels
func (r *GatewayReconciler) getNamespaceLabels(ctx context.Context, ns string) (map[string]string, error) {
	var namespace corev1.Namespace
	if err := r.Get(ctx, client.ObjectKey{Name: ns}, &namespace); err != nil {
		return nil, err
	}
	return namespace.Labels, nil
}

// setGatewayStatusCondition helper for set status condition
func (r *GatewayReconciler) setGatewayStatusCondition(
	ctx context.Context,
	gw *gatewayv1.Gateway,
	condType, reason, message string,
	status metav1.ConditionStatus,
) error {
	orig := gw.DeepCopy()
	cond := metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: gw.Generation,
	}
	meta.SetStatusCondition(&gw.Status.Conditions, cond)
	return r.Status().Patch(ctx, gw, client.MergeFrom(orig))
}
