package controller

import (
	"errors"
	"testing"

	"github.com/serverscom/api-gateway-controller/internal/types"

	. "github.com/onsi/gomega"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func ptrTLSMode(m gatewayv1.TLSModeType) *gatewayv1.TLSModeType {
	x := m
	return &x
}

func Test_hostMatches(t *testing.T) {
	g := NewWithT(t)

	g.Expect(hostMatches("", "a.example.com")).To(BeTrue())
	g.Expect(hostMatches("example.com", "example.com")).To(BeTrue())
	g.Expect(hostMatches("*.example.com", "sub.example.com")).To(BeTrue())
	g.Expect(hostMatches("*.example.com", "example.com")).To(BeFalse())
	g.Expect(hostMatches("foo.example.com", "bar.example.com")).To(BeFalse())
}

func Test_validateHTTPSListener(t *testing.T) {
	g := NewWithT(t)

	// valid listener
	listener := gatewayv1.Listener{
		Protocol: gatewayv1.HTTPSProtocolType,
		Hostname: ptrHostname("example.com"),
		TLS: &gatewayv1.GatewayTLSConfig{
			Mode: ptrTLSMode(gatewayv1.TLSModeTerminate),
		},
	}
	g.Expect(validateHTTPSListener(listener)).To(BeNil())

	// missing hostname
	l1 := listener
	l1.Hostname = nil
	g.Expect(validateHTTPSListener(l1)).ToNot(BeNil())

	// missing TLS
	l2 := listener
	l2.TLS = nil
	g.Expect(validateHTTPSListener(l2)).ToNot(BeNil())

	// wrong mode
	l3 := listener
	l3.TLS = &gatewayv1.GatewayTLSConfig{Mode: ptrTLSMode(gatewayv1.TLSModePassthrough)}
	g.Expect(validateHTTPSListener(l3)).ToNot(BeNil())
}

func Test_joinErrors(t *testing.T) {
	g := NewWithT(t)
	errs := []error{errors.New("one"), errors.New("two")}
	out := joinErrors(errs)
	g.Expect(out).To(ContainSubstring("one"))
	g.Expect(out).To(ContainSubstring("two"))
	g.Expect(out).To(ContainSubstring("- "))
}

func TestIsRouteAttachedToGateway(t *testing.T) {
	g := NewWithT(t)

	route := &gatewayv1.HTTPRoute{}
	route.ObjectMeta.SetNamespace("routes-ns")
	route.Spec.ParentRefs = []gatewayv1.ParentReference{
		{
			Name: gatewayv1.ObjectName("gw1"),
		},
	}
	gw := &gatewayv1.Gateway{}
	gw.ObjectMeta.SetName("gw1")
	gw.ObjectMeta.SetNamespace("routes-ns")
	g.Expect(isRouteAttachedToGateway(route, gw)).To(BeTrue())

	route2 := route.DeepCopy()
	route2.Spec.ParentRefs = []gatewayv1.ParentReference{
		{
			Name:      gatewayv1.ObjectName("gw1"),
			Namespace: func() *gatewayv1.Namespace { n := gatewayv1.Namespace("other-ns"); return &n }(),
		},
	}
	g.Expect(isRouteAttachedToGateway(route2, gw)).To(BeFalse())
}

func ListenerInfoForTest() types.ListenerInfo {
	return types.ListenerInfo{
		Name:        "l",
		Hostname:    "",
		Protocol:    "HTTP",
		Port:        80,
		AllowedFrom: "Same",
		Selector:    map[string]string{},
	}
}

func TestIsRouteNamespaceAllowed(t *testing.T) {
	g := NewWithT(t)

	// allowedFrom = All
	listener := ListenerInfoForTest()
	listener.AllowedFrom = "All"
	g.Expect(isRouteNamespaceAllowed(listener, "a", "b", nil)).To(BeTrue())

	// allowedFrom = Same
	listener = ListenerInfoForTest()
	listener.AllowedFrom = "Same"
	g.Expect(isRouteNamespaceAllowed(listener, "ns1", "ns1", nil)).To(BeTrue())
	g.Expect(isRouteNamespaceAllowed(listener, "ns1", "ns2", nil)).To(BeFalse())

	// allowedFrom = Selector
	listener = ListenerInfoForTest()
	listener.AllowedFrom = "Selector"
	listener.Selector = map[string]string{"team": "alpha"}
	nsLabels := map[string]string{"team": "alpha"}
	g.Expect(isRouteNamespaceAllowed(listener, "x", "y", nsLabels)).To(BeTrue())

	nsLabels = map[string]string{"team": "beta"}
	g.Expect(isRouteNamespaceAllowed(listener, "x", "y", nsLabels)).To(BeFalse())
}
