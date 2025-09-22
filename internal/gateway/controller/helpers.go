package controller

import (
	"fmt"
	"strings"

	"github.com/serverscom/api-gateway-controller/internal/types"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// isRouteAttachedToGateway returns true if route is attached to Gateway
func isRouteAttachedToGateway(route *gatewayv1.HTTPRoute, gw *gatewayv1.Gateway) bool {
	for _, parent := range route.Spec.ParentRefs {
		if parent.Kind != nil && string(*parent.Kind) != "Gateway" {
			continue
		}
		if parent.Group != nil && *parent.Group != gatewayv1.GroupName {
			continue
		}
		if string(parent.Name) != gw.Name {
			continue
		}

		ns := route.Namespace
		if parent.Namespace != nil {
			ns = string(*parent.Namespace)
		}
		if ns == gw.Namespace {
			return true
		}
	}
	return false
}

// isRouteNamespaceAllowed returns true if route's namespace is permitted by the listener policy.
func isRouteNamespaceAllowed(listener types.ListenerInfo, listenerNS, routeNS string, nsLabels map[string]string) bool {
	switch listener.AllowedFrom {
	case "All":
		return true
	case "Same":
		return listenerNS == routeNS
	case "Selector":
		for k, v := range listener.Selector {
			if nsLabels[k] != v {
				return false
			}
		}
		return true
	}
	return listenerNS == routeNS
}

// validateHTTPSListener validates https listener
func validateHTTPSListener(listener gatewayv1.Listener) error {
	if listener.Protocol != gatewayv1.HTTPSProtocolType {
		return nil
	}
	if listener.Hostname == nil || *listener.Hostname == "" {
		return fmt.Errorf("hostname must be specified for HTTPS protocol")
	}
	if listener.TLS == nil {
		return fmt.Errorf("hostname=%q: missing TLS config", *listener.Hostname)
	}
	if listener.TLS.Mode == nil || *listener.TLS.Mode != gatewayv1.TLSModeTerminate {
		return fmt.Errorf("hostname=%q: TLS mode must be 'Terminate'", *listener.Hostname)
	}
	return nil
}

// joinErrors helpers to join errors
func joinErrors(errs []error) string {
	var b strings.Builder
	for _, err := range errs {
		b.WriteString("- ")
		b.WriteString(err.Error())
		b.WriteString("\n")
	}
	return b.String()
}

// hostMatches reports whether routeHost matches listenerHost, supporting wildcards.
func hostMatches(listenerHost, routeHost string) bool {
	if listenerHost == "" {
		return true
	}
	if listenerHost == routeHost {
		return true
	}
	if strings.HasPrefix(listenerHost, "*.") && len(listenerHost) > 2 {
		suffix := listenerHost[1:]
		return strings.HasSuffix(routeHost, suffix)
	}
	return false
}
