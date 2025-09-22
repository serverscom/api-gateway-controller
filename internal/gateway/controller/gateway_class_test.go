package controller

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func Test_GatewayClassReconciler_Reconcile_SetsAccepted(t *testing.T) {
	g := NewWithT(t)
	scheme := setupScheme(t)

	gc := gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-gc",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: gatewayv1.GatewayController("example.com/controller"),
		},
	}

	fakeCli := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&gatewayv1.GatewayClass{}).
		WithObjects(&gc).
		Build()

	r := &GatewayClassReconciler{
		Client:         fakeCli,
		ControllerName: "example.com/controller",
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: gc.Name}}

	_, err := r.Reconcile(context.Background(), req)
	g.Expect(err).To(BeNil())

	var got gatewayv1.GatewayClass
	g.Expect(fakeCli.Get(context.Background(), types.NamespacedName{Name: gc.Name}, &got)).To(Succeed())
	found := false
	for _, c := range got.Status.Conditions {
		if c.Reason == "Accepted" {
			found = true
		}
	}
	g.Expect(found).To(BeTrue())
}

func Test_GatewayClassReconciler_Reconcile_SkipsOtherController(t *testing.T) {
	g := NewWithT(t)
	scheme := setupScheme(t)

	gc := gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "another-gc",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: gatewayv1.GatewayController("other/controller"),
		},
	}

	fakeCli := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&gatewayv1.GatewayClass{}).
		WithObjects(&gc).
		Build()

	r := &GatewayClassReconciler{
		Client:         fakeCli,
		ControllerName: "example.com/controller",
	}

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: gc.Name}}
	_, err := r.Reconcile(context.Background(), req)
	g.Expect(err).To(BeNil())

	var got gatewayv1.GatewayClass
	g.Expect(fakeCli.Get(context.Background(), types.NamespacedName{Name: gc.Name}, &got)).To(Succeed())
	g.Expect(len(got.Status.Conditions)).To(Equal(0))
}
