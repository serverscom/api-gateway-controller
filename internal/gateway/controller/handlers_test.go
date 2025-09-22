package controller

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func Test_getParentGatewayKeys(t *testing.T) {
	g := NewWithT(t)
	route := &gatewayv1.HTTPRoute{}
	route.ObjectMeta.SetNamespace("rns")
	pr := gatewayv1.ParentReference{
		Name: gatewayv1.ObjectName("gw1"),
	}
	route.Spec.ParentRefs = []gatewayv1.ParentReference{pr}
	r := &GatewayReconciler{}
	keys := r.getParentGatewayKeys(route)
	g.Expect(keys).To(ContainElement("rns/gw1"))
	// explicit namespace
	pr.Namespace = func() *gatewayv1.Namespace { n := gatewayv1.Namespace("gw-ns"); return &n }()
	route.Spec.ParentRefs = []gatewayv1.ParentReference{pr}
	keys = r.getParentGatewayKeys(route)
	g.Expect(keys).To(ContainElement("gw-ns/gw1"))
}

func Test_routeReferencesService(t *testing.T) {
	g := NewWithT(t)
	route := &gatewayv1.HTTPRoute{}
	route.ObjectMeta.SetNamespace("ns")
	route.Spec.Rules = []gatewayv1.HTTPRouteRule{
		{
			BackendRefs: []gatewayv1.HTTPBackendRef{
				{
					BackendRef: gatewayv1.BackendRef{
						BackendObjectReference: gatewayv1.BackendObjectReference{
							Name: gatewayv1.ObjectName("svc"),
						},
					},
				},
			},
		},
	}
	svc := &corev1.Service{}
	svc.ObjectMeta.SetNamespace("ns")
	svc.ObjectMeta.SetName("svc")
	gr := &GatewayReconciler{}
	g.Expect(gr.routeReferencesService(route, svc)).To(BeTrue())
}

func Test_gatewayReferencesSecret(t *testing.T) {
	g := NewWithT(t)
	gw := &gatewayv1.Gateway{}
	gw.ObjectMeta.SetNamespace("ns")
	gw.Spec.Listeners = []gatewayv1.Listener{
		{
			TLS: &gatewayv1.GatewayTLSConfig{
				CertificateRefs: []gatewayv1.SecretObjectReference{
					{
						Name: gatewayv1.ObjectName("s1"),
					},
				},
			},
		},
	}
	secret := &corev1.Secret{}
	secret.ObjectMeta.SetNamespace("ns")
	secret.ObjectMeta.SetName("s1")
	gr := &GatewayReconciler{}
	g.Expect(gr.gatewayReferencesSecret(gw, secret)).To(BeTrue())
	secret.ObjectMeta.SetNamespace("other")
	g.Expect(gr.gatewayReferencesSecret(gw, secret)).To(BeFalse())
}

func Test_findGatewaysForHTTPRoute(t *testing.T) {
	g := NewWithT(t)
	scheme := setupScheme(t)
	// prepare GatewayClass, Gateway, HTTPRoute
	gc := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{Name: "gc1"},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: gatewayv1.GatewayController("example.com/controller"),
		},
	}
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw1", Namespace: "gw-ns"},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: gatewayv1.ObjectName("gc1"),
		},
	}
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "gw-ns"},
		Spec: gatewayv1.HTTPRouteSpec{
			Hostnames: []gatewayv1.Hostname{"example.com"},
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{
					{
						Name: gatewayv1.ObjectName("gw1"),
					},
				},
			},
		},
	}
	fakeCli := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(gc, gw, route).
		Build()
	r := &GatewayReconciler{
		Client:         fakeCli,
		ControllerName: "example.com/controller",
	}
	reqs := r.findGatewaysForHTTPRoute(context.Background(), route)
	g.Expect(len(reqs)).To(Equal(1))
	g.Expect(reqs[0].NamespacedName.Name).To(Equal("gw1"))
	g.Expect(reqs[0].NamespacedName.Namespace).To(Equal("gw-ns"))
}

func Test_findGatewaysForService(t *testing.T) {
	g := NewWithT(t)
	scheme := setupScheme(t)
	gc := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{Name: "gc1"},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: gatewayv1.GatewayController("example.com/controller"),
		},
	}
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw1", Namespace: "gw-ns"},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: gatewayv1.ObjectName("gc1"),
		},
	}
	routeWithBackend := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "route-backend", Namespace: "gw-ns"},
		Spec: gatewayv1.HTTPRouteSpec{
			Rules: []gatewayv1.HTTPRouteRule{
				{
					BackendRefs: []gatewayv1.HTTPBackendRef{
						{
							BackendRef: gatewayv1.BackendRef{
								BackendObjectReference: gatewayv1.BackendObjectReference{
									Name: gatewayv1.ObjectName("svc"),
								},
							},
						},
					},
				},
			},
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{
					{
						Name: gatewayv1.ObjectName("gw1"),
					},
				},
			},
		},
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "gw-ns"},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{Port: 80, NodePort: 30080}},
		},
	}
	fakeCli := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(gc, gw, routeWithBackend, svc).
		Build()
	r := &GatewayReconciler{
		Client:         fakeCli,
		ControllerName: "example.com/controller",
	}
	reqs := r.findGatewaysForService(context.Background(), svc)
	g.Expect(len(reqs)).To(Equal(1))
	g.Expect(reqs[0].NamespacedName.Name).To(Equal("gw1"))
}

func Test_findGatewaysForSecret(t *testing.T) {
	g := NewWithT(t)
	scheme := setupScheme(t)
	gc := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{Name: "gc1"},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: gatewayv1.GatewayController("example.com/controller"),
		},
	}
	gwWithSecret := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw1", Namespace: "gw-ns"},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: gatewayv1.ObjectName("gc1"),
			Listeners: []gatewayv1.Listener{
				{
					TLS: &gatewayv1.GatewayTLSConfig{
						CertificateRefs: []gatewayv1.SecretObjectReference{
							{Name: gatewayv1.ObjectName("s1")},
						},
					},
				},
			},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "gw-ns"},
	}
	fakeCli := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(gc, gwWithSecret, secret).
		Build()
	r := &GatewayReconciler{
		Client:         fakeCli,
		ControllerName: "example.com/controller",
	}
	reqs := r.findGatewaysForSecret(context.Background(), secret)
	g.Expect(len(reqs)).To(Equal(1))
	g.Expect(reqs[0].NamespacedName.Name).To(Equal("gw1"))
}
