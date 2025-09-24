package controller

import (
	"context"
	"testing"

	"github.com/serverscom/api-gateway-controller/internal/config"
	"github.com/serverscom/api-gateway-controller/internal/mocks"

	. "github.com/onsi/gomega"
	serverscom "github.com/serverscom/serverscom-go-client/pkg"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

var (
	testGwNs = "test-gw-ns"
	testGw   = "test-gw"
)

func setupScheme(t *testing.T) *runtime.Scheme {
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(clientgoscheme.AddToScheme(scheme)).To(BeNil())
	g.Expect(gatewayv1.Install(scheme)).To(BeNil())
	g.Expect(corev1.AddToScheme(scheme)).To(BeNil())
	return scheme
}

func ptrHostname(s string) *gatewayv1.Hostname {
	h := gatewayv1.Hostname(s)
	return &h
}

func TestReconcile(t *testing.T) {
	s := setupScheme(t)
	baseGC := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: config.DEFAULT_GATEWAY_CLASS,
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: gatewayv1.GatewayController(config.DEFAULT_CONTROLLER_NAME),
		},
	}
	baseGW := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testGw,
			Namespace: testGwNs,
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: gatewayv1.ObjectName(config.DEFAULT_GATEWAY_CLASS),
			Listeners: []gatewayv1.Listener{{
				Name:     "https",
				Port:     443,
				Protocol: gatewayv1.HTTPSProtocolType,
				Hostname: ptrHostname("example.com"),
			}},
		},
	}
	tests := []struct {
		name        string
		prepareObjs func() []client.Object
		setupMocks  func(tls *mocks.MockTLSManagerInterface, lb *mocks.MockLBManagerInterface)
		checkStatus func(t *testing.T, cli client.Client)
		expectError bool
	}{
		{
			name: "https listener valid tls",
			prepareObjs: func() []client.Object {
				gw := baseGW.DeepCopy()
				mode := gatewayv1.TLSModeTerminate
				gw.Spec.Listeners[0].TLS = &gatewayv1.GatewayTLSConfig{
					Mode: &mode,
					Options: map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue{
						config.TLS_EXTERNAL_ID_KEY: "ext-cert-123",
					},
				}
				return []client.Object{baseGC.DeepCopy(), gw}
			},
			setupMocks: func(tls *mocks.MockTLSManagerInterface, lb *mocks.MockLBManagerInterface) {
				tls.EXPECT().
					EnsureTLS(gomock.Any(), gomock.Any()).
					Return(map[string]string{"example.com": "ext-cert-123"}, nil)
				lb.EXPECT().
					EnsureLB(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&serverscom.L7LoadBalancer{ID: "lb-1", Status: config.LB_ACTIVE_STATUS}, nil)
			},
			checkStatus: func(t *testing.T, cli client.Client) {
				var gw gatewayv1.Gateway
				if err := cli.Get(context.Background(), types.NamespacedName{Name: testGw, Namespace: testGwNs}, &gw); err != nil {
					t.Fatalf("get gw failed: %v", err)
				}
				progFound := false
				for _, c := range gw.Status.Conditions {
					if c.Type == "Programmed" && c.Status == metav1.ConditionTrue {
						progFound = true
					}
				}
				if !progFound {
					t.Errorf("expected Programmed=True condition")
				}
			},
		},
		{
			name: "http listener only",
			prepareObjs: func() []client.Object {
				gw := baseGW.DeepCopy()
				gw.Spec.Listeners = []gatewayv1.Listener{{
					Name:     "http",
					Port:     80,
					Protocol: gatewayv1.HTTPProtocolType,
					Hostname: nil,
					TLS:      nil,
				}}
				return []client.Object{baseGC.DeepCopy(), gw}
			},
			setupMocks: func(tls *mocks.MockTLSManagerInterface, lb *mocks.MockLBManagerInterface) {
				lb.EXPECT().
					EnsureLB(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&serverscom.L7LoadBalancer{ID: "lb-2", Status: config.LB_ACTIVE_STATUS}, nil)
				tls.EXPECT().
					EnsureTLS(gomock.Any(), gomock.Any()).
					Return(map[string]string{}, nil)
			},
			checkStatus: func(t *testing.T, cli client.Client) {
				var gw gatewayv1.Gateway
				if err := cli.Get(context.Background(), types.NamespacedName{Name: testGw, Namespace: testGwNs}, &gw); err != nil {
					t.Fatalf("get gw failed: %v", err)
				}
				for _, c := range gw.Status.Conditions {
					if c.Type == "Programmed" && c.Status == metav1.ConditionTrue {
						return
					}
				}
				t.Errorf("expected Programmed=True condition on HTTP listener")
			},
		},
		{
			name: "https missing tls",
			prepareObjs: func() []client.Object {
				gw := baseGW.DeepCopy()
				return []client.Object{baseGC.DeepCopy(), gw}
			},
			setupMocks: func(tls *mocks.MockTLSManagerInterface, lb *mocks.MockLBManagerInterface) {},
			checkStatus: func(t *testing.T, cli client.Client) {
				var gw gatewayv1.Gateway
				_ = cli.Get(context.Background(), types.NamespacedName{Name: testGw, Namespace: testGwNs}, &gw)
				found := false
				for _, c := range gw.Status.Conditions {
					if c.Type == "Accepted" && c.Status == metav1.ConditionFalse {
						found = true
					}
				}
				if !found {
					t.Errorf("expected Accepted=False on missing TLS")
				}
			},
			expectError: false,
		},
		{
			name: "https wrong tls mode",
			prepareObjs: func() []client.Object {
				gw := baseGW.DeepCopy()
				mode := gatewayv1.TLSModePassthrough
				gw.Spec.Listeners[0].TLS = &gatewayv1.GatewayTLSConfig{Mode: &mode}
				return []client.Object{baseGC.DeepCopy(), gw}
			},
			setupMocks: func(tls *mocks.MockTLSManagerInterface, lb *mocks.MockLBManagerInterface) {},
			checkStatus: func(t *testing.T, cli client.Client) {
				var gw gatewayv1.Gateway
				_ = cli.Get(context.Background(), types.NamespacedName{Name: testGw, Namespace: testGwNs}, &gw)
				found := false
				for _, c := range gw.Status.Conditions {
					if c.Type == "Accepted" && c.Status == metav1.ConditionFalse {
						found = true
					}
				}
				if !found {
					t.Errorf("expected Accepted=False on wrong TLS mode")
				}
			},
			expectError: false,
		},
		{
			name: "not managed gateway",
			prepareObjs: func() []client.Object {
				gw := baseGW.DeepCopy()
				gw.Spec.GatewayClassName = "some-other-class"
				return []client.Object{baseGC.DeepCopy(), gw}
			},
			setupMocks: func(tls *mocks.MockTLSManagerInterface, lb *mocks.MockLBManagerInterface) {
				lb.EXPECT().
					DeleteLB(gomock.Any(), gomock.Any()).
					Return(nil)
			},
			checkStatus: func(t *testing.T, cli client.Client) {
				var gw gatewayv1.Gateway
				_ = cli.Get(context.Background(), types.NamespacedName{Name: testGw, Namespace: testGwNs}, &gw)
				noLonger := false
				for _, c := range gw.Status.Conditions {
					if c.Type == "Programmed" && c.Status == metav1.ConditionFalse && c.Reason == "NoLongerManaged" {
						noLonger = true
					}
				}
				if !noLonger {
					t.Errorf("expected Programmed=False, Reason=NoLongerManaged")
				}
			},
			expectError: false,
		},
		{
			name: "http and https listeners",
			prepareObjs: func() []client.Object {
				gw := baseGW.DeepCopy()
				term := gatewayv1.TLSModeTerminate
				gw.Spec.Listeners = []gatewayv1.Listener{
					{
						Name:     "http",
						Port:     80,
						Protocol: gatewayv1.HTTPProtocolType,
					},
					{
						Name:     "https",
						Port:     443,
						Protocol: gatewayv1.HTTPSProtocolType,
						Hostname: ptrHostname("foo.com"),
						TLS: &gatewayv1.GatewayTLSConfig{
							Mode: &term,
							Options: map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue{
								config.TLS_EXTERNAL_ID_KEY: "ext-cert-123",
							},
						},
					},
				}
				return []client.Object{baseGC.DeepCopy(), gw}
			},
			setupMocks: func(tls *mocks.MockTLSManagerInterface, lb *mocks.MockLBManagerInterface) {
				tls.EXPECT().
					EnsureTLS(gomock.Any(), gomock.Any()).
					Return(map[string]string{"foo.com": "ext-cert-123"}, nil)
				lb.EXPECT().
					EnsureLB(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&serverscom.L7LoadBalancer{ID: "lb-4", Status: config.LB_ACTIVE_STATUS}, nil)
			},
			checkStatus: func(t *testing.T, cli client.Client) {
				var gw gatewayv1.Gateway
				_ = cli.Get(context.Background(), types.NamespacedName{Name: testGw, Namespace: testGwNs}, &gw)
				ok := false
				for _, c := range gw.Status.Conditions {
					if c.Type == "Programmed" && c.Status == metav1.ConditionTrue {
						ok = true
					}
				}
				if !ok {
					t.Errorf("expected Programmed=True on HTTP + HTTPS listeners")
				}
			},
			expectError: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrlr := gomock.NewController(t)
			defer ctrlr.Finish()
			mockTLS := mocks.NewMockTLSManagerInterface(ctrlr)
			mockLB := mocks.NewMockLBManagerInterface(ctrlr)
			tt.setupMocks(mockTLS, mockLB)
			fakeCli := fake.NewClientBuilder().
				WithScheme(s).
				WithStatusSubresource(&gatewayv1.Gateway{}).
				WithObjects(tt.prepareObjs()...).
				Build()
			recorder := record.NewFakeRecorder(16)
			r := &GatewayReconciler{
				Client:           fakeCli,
				ControllerName:   config.DEFAULT_CONTROLLER_NAME,
				GatewayClassName: config.DEFAULT_GATEWAY_CLASS,
				TLSMgr:           mockTLS,
				LBMgr:            mockLB,
				Recorder:         recorder,
			}
			_, err := r.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: testGwNs,
					Name:      testGw,
				},
			})
			if tt.expectError && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			tt.checkStatus(t, fakeCli)
		})
	}
}

func TestReconcile_LBBecomesActiveOnSecondPass(t *testing.T) {
	s := setupScheme(t)
	baseGC := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: config.DEFAULT_GATEWAY_CLASS,
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: gatewayv1.GatewayController(config.DEFAULT_CONTROLLER_NAME),
		},
	}
	baseGW := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testGw,
			Namespace: testGwNs,
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: gatewayv1.ObjectName(config.DEFAULT_GATEWAY_CLASS),
			Listeners: []gatewayv1.Listener{{
				Name:     "http",
				Port:     80,
				Protocol: gatewayv1.HTTPProtocolType,
				Hostname: ptrHostname("example.com"),
			}},
		},
	}

	ctrlr := gomock.NewController(t)
	defer ctrlr.Finish()

	mockTLS := mocks.NewMockTLSManagerInterface(ctrlr)
	mockLB := mocks.NewMockLBManagerInterface(ctrlr)
	fakeCli := fake.NewClientBuilder().
		WithScheme(s).
		WithStatusSubresource(&gatewayv1.Gateway{}).
		WithObjects(baseGC.DeepCopy(), baseGW.DeepCopy()).
		Build()

	recorder := record.NewFakeRecorder(8)
	r := &GatewayReconciler{
		Client:           fakeCli,
		ControllerName:   config.DEFAULT_CONTROLLER_NAME,
		GatewayClassName: config.DEFAULT_GATEWAY_CLASS,
		TLSMgr:           mockTLS,
		LBMgr:            mockLB,
		Recorder:         recorder,
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: testGwNs,
			Name:      testGw,
		},
	}

	mockTLS.EXPECT().
		EnsureTLS(gomock.Any(), gomock.Any()).
		Return(map[string]string{"example.com": "cert-id"}, nil)
	mockLB.EXPECT().
		EnsureLB(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(&serverscom.L7LoadBalancer{ID: "lb-5", Status: "pending"}, nil)

	res, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.RequeueAfter == 0 {
		t.Errorf("expected RequeueAfter > 0 due to non-active LB status")
	}

	mockTLS.EXPECT().
		EnsureTLS(gomock.Any(), gomock.Any()).
		Return(map[string]string{"example.com": "cert-id"}, nil)
	mockLB.EXPECT().
		EnsureLB(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(&serverscom.L7LoadBalancer{ID: "lb-5", Status: config.LB_ACTIVE_STATUS}, nil)

	res, err = r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error on second pass: %v", err)
	}
	if res.RequeueAfter != 0 && !res.Requeue {
		t.Errorf("expected no requeue, got requeueAfter=%v", res.RequeueAfter)
	}

	var gw gatewayv1.Gateway
	if err := fakeCli.Get(context.Background(), req.NamespacedName, &gw); err != nil {
		t.Fatalf("get gw failed: %v", err)
	}
	hasProgrammed := false
	for _, c := range gw.Status.Conditions {
		if c.Type == "Programmed" && c.Status == metav1.ConditionTrue {
			hasProgrammed = true
		}
	}
	if !hasProgrammed {
		t.Errorf("expected Programmed=True condition after LB becomes Active")
	}
}

func Test_buildTLSInfo(t *testing.T) {
	g := NewWithT(t)
	scheme := setupScheme(t)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: testGwNs},
		Data:       map[string][]byte{"tls.crt": []byte("x"), "tls.key": []byte("y")},
	}

	// gw1: with secret ref
	gw1 := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw1", Namespace: testGwNs},
		Spec: gatewayv1.GatewaySpec{
			Listeners: []gatewayv1.Listener{{
				Name:     "https",
				Protocol: gatewayv1.HTTPSProtocolType,
				Hostname: func() *gatewayv1.Hostname { h := gatewayv1.Hostname("secret.com"); return &h }(),
				TLS: &gatewayv1.GatewayTLSConfig{
					Mode: ptrTLSMode(gatewayv1.TLSModeTerminate),
					CertificateRefs: []gatewayv1.SecretObjectReference{
						{Name: "s1"},
					},
				},
				Port: 443,
			}},
		},
	}

	// gw2: with external cert id
	gw2 := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw2", Namespace: testGwNs},
		Spec: gatewayv1.GatewaySpec{
			Listeners: []gatewayv1.Listener{{
				Name:     "https2",
				Protocol: gatewayv1.HTTPSProtocolType,
				Hostname: func() *gatewayv1.Hostname { h := gatewayv1.Hostname("external.com"); return &h }(),
				TLS: &gatewayv1.GatewayTLSConfig{
					Mode: ptrTLSMode(gatewayv1.TLSModeTerminate),
					Options: map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue{
						config.TLS_EXTERNAL_ID_KEY: "ext-cert-123",
					},
				},
				Port: 443,
			}},
		},
	}

	fakeCli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
	r := &GatewayReconciler{Client: fakeCli}

	// case 1: secret ref
	tlsMap1, err := r.buildTLSInfo(context.Background(), gw1)
	g.Expect(err).To(BeNil())
	g.Expect(tlsMap1).To(HaveKey("secret.com"))
	g.Expect(tlsMap1["secret.com"].Secret).ToNot(BeNil())
	g.Expect(tlsMap1["secret.com"].ExternalID).To(Equal(""))

	// case 2: external id
	tlsMap2, err := r.buildTLSInfo(context.Background(), gw2)
	g.Expect(err).To(BeNil())
	g.Expect(tlsMap2).To(HaveKey("external.com"))
	g.Expect(tlsMap2["external.com"].ExternalID).To(Equal("ext-cert-123"))
	g.Expect(tlsMap2["external.com"].Secret).To(BeNil())
}

func Test_buildGatewayInfo(t *testing.T) {
	g := NewWithT(t)
	scheme := setupScheme(t)

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "n1"},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "10.0.0.10"}},
		},
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testGwNs}}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: testGwNs},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeNodePort,
			Ports: []corev1.ServicePort{{
				Name:     "http",
				Port:     80,
				NodePort: 30080,
			}},
		},
	}

	// gw: HTTP listener
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw1", Namespace: testGwNs},
		Spec: gatewayv1.GatewaySpec{
			Listeners: []gatewayv1.Listener{{
				Name:     "l1",
				Protocol: gatewayv1.HTTPProtocolType,
				Port:     80,
			}},
		},
	}

	// gw with HTTPS
	gwTLS := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw2", Namespace: testGwNs},
		Spec: gatewayv1.GatewaySpec{
			Listeners: []gatewayv1.Listener{{
				Name:     "l2",
				Protocol: gatewayv1.HTTPSProtocolType,
				Port:     443,
				Hostname: func() *gatewayv1.Hostname { h := gatewayv1.Hostname("tls.com"); return &h }(),
				TLS:      &gatewayv1.GatewayTLSConfig{Mode: ptrTLSMode(gatewayv1.TLSModeTerminate)},
			}},
		},
	}

	// route matched gw
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "r1", Namespace: testGwNs},
		Spec: gatewayv1.HTTPRouteSpec{
			Hostnames: []gatewayv1.Hostname{"example.com"},
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{
					{
						Name:      gatewayv1.ObjectName("gw1"),
						Namespace: func() *gatewayv1.Namespace { n := gatewayv1.Namespace(testGwNs); return &n }(),
					},
				},
			},
			Rules: []gatewayv1.HTTPRouteRule{{
				BackendRefs: []gatewayv1.HTTPBackendRef{
					{
						BackendRef: gatewayv1.BackendRef{
							BackendObjectReference: gatewayv1.BackendObjectReference{
								Name: gatewayv1.ObjectName("svc"),
							},
						},
					},
				},
			}},
		},
	}

	// route with unmatched host
	routeUnmatched := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "r2", Namespace: "routes-ns"},
		Spec: gatewayv1.HTTPRouteSpec{
			Hostnames: []gatewayv1.Hostname{"no-match.com"},
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{
					{
						Name: gatewayv1.ObjectName("gw1"),
					},
				},
			},
		},
	}

	// route with no backend
	routeNoBackend := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "r3", Namespace: "routes-ns"},
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
		WithObjects(node, ns, svc, gw, gwTLS, route, routeUnmatched, routeNoBackend).
		Build()

	r := &GatewayReconciler{Client: fakeCli}

	// case 1: HTTP
	gi1, err := r.buildGatewayInfo(context.Background(), gw)
	g.Expect(err).To(BeNil())
	g.Expect(gi1.VHosts).To(HaveKey("example.com"))
	g.Expect(gi1.VHosts["example.com"].SSL).To(BeFalse())

	// case 2: HTTPS
	gi2, err := r.buildGatewayInfo(context.Background(), gwTLS)
	g.Expect(err).To(BeNil())
	g.Expect(gi2.VHosts).To(BeEmpty())

	// case 3: unmatched host
	gi3, err := r.buildGatewayInfo(context.Background(), gw)
	g.Expect(err).To(BeNil())
	g.Expect(gi3.VHosts).To(HaveKey("example.com"))
	g.Expect(gi3.VHosts).ToNot(HaveKey("no-match.com"))
}
