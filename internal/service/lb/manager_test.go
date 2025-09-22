package lbsrv

import (
	"context"
	"errors"
	"testing"

	. "github.com/onsi/gomega"
	serverscom "github.com/serverscom/serverscom-go-client/pkg"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/serverscom/api-gateway-controller/internal/config"
	"github.com/serverscom/api-gateway-controller/internal/mocks"
	"github.com/serverscom/api-gateway-controller/internal/types"

	"go.uber.org/mock/gomock"
)

func TestEnsureLB(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	lbHandler := mocks.NewMockLoadBalancersService(mockCtrl)
	collectionHandler := mocks.NewMockCollection[serverscom.LoadBalancer](mockCtrl)

	client := serverscom.NewClientWithEndpoint("", "")
	client.LoadBalancers = lbHandler
	manager := NewManager(client)

	gwInfo := &types.GatewayInfo{
		UID:  "gw-uid",
		Name: "gw-name",
		NS:   "default",
		VHosts: map[string]*types.VHostInfo{
			"example.com": {
				Host: "example.com",
				SSL:  true,
				Ports: []int32{
					443,
				},
				Paths: []types.PathInfo{
					{
						Path: "/",
						Service: &corev1.Service{
							ObjectMeta: metav1.ObjectMeta{
								Name: "svc",
							},
						},
						NodePort: 8080,
						NodeIps:  []string{"1.1.1.1"},
					},
				},
			},
		},
	}

	tests := []struct {
		name       string
		setupMocks func()
		wantErr    bool
		wantID     string
		wantStatus string
	}{
		{
			name: "error on list lbs",
			setupMocks: func() {
				lbHandler.EXPECT().
					Collection().
					Return(collectionHandler)
				collectionHandler.EXPECT().
					SetParam("type", "l7").
					Return(collectionHandler)
				collectionHandler.EXPECT().
					SetParam("label_selector", config.GW_LABEL_ID+"="+gwInfo.UID).
					Return(collectionHandler)
				collectionHandler.EXPECT().
					Collect(gomock.Any()).
					Return(nil, errors.New("list error"))
			},
			wantErr: true,
		},
		{
			name: "create new lb",
			setupMocks: func() {
				lbHandler.EXPECT().
					Collection().
					Return(collectionHandler)
				collectionHandler.EXPECT().
					SetParam("type", "l7").
					Return(collectionHandler)
				collectionHandler.EXPECT().
					SetParam("label_selector", config.GW_LABEL_ID+"="+gwInfo.UID).
					Return(collectionHandler)
				collectionHandler.EXPECT().
					Collect(gomock.Any()).
					Return(nil, nil)

				lbHandler.EXPECT().
					CreateL7LoadBalancer(gomock.Any(), gomock.Any()).
					Return(&serverscom.L7LoadBalancer{ID: "new-lb", Status: config.LB_ACTIVE_STATUS}, nil)
			},
			wantID: "new-lb",
		},
		{
			name: "multiple lbs found",
			setupMocks: func() {
				lbHandler.EXPECT().
					Collection().
					Return(collectionHandler)
				collectionHandler.EXPECT().
					SetParam(gomock.Any(), gomock.Any()).
					AnyTimes().
					Return(collectionHandler)
				collectionHandler.EXPECT().
					Collect(gomock.Any()).
					Return([]serverscom.LoadBalancer{
						{ID: "lb1"}, {ID: "lb2"},
					}, nil)
			},
			wantErr: true,
		},
		{
			name: "lb not active yet",
			setupMocks: func() {
				lbHandler.EXPECT().
					Collection().
					Return(collectionHandler)
				collectionHandler.EXPECT().
					SetParam(gomock.Any(), gomock.Any()).
					AnyTimes().
					Return(collectionHandler)
				collectionHandler.EXPECT().
					Collect(gomock.Any()).
					Return([]serverscom.LoadBalancer{
						{ID: "lb1", Status: "pending"},
					}, nil)
			},
			wantStatus: "pending",
		},
		{
			name: "update existing lb",
			setupMocks: func() {
				lbHandler.EXPECT().
					Collection().
					Return(collectionHandler)
				collectionHandler.EXPECT().
					SetParam(gomock.Any(), gomock.Any()).
					AnyTimes().
					Return(collectionHandler)
				collectionHandler.EXPECT().
					Collect(gomock.Any()).
					Return([]serverscom.LoadBalancer{
						{ID: "lb1", Status: config.LB_ACTIVE_STATUS},
					}, nil)

				lbHandler.EXPECT().
					UpdateL7LoadBalancer(gomock.Any(), "lb1", gomock.Any()).
					Return(&serverscom.L7LoadBalancer{ID: "lb1", Status: config.LB_ACTIVE_STATUS}, nil)
			},
			wantID:     "lb1",
			wantStatus: config.LB_ACTIVE_STATUS,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			tt.setupMocks()

			res, err := manager.EnsureLB(context.Background(), gwInfo, map[string]string{"example.com": "cert-id"})
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).To(BeNil())

			if tt.wantID != "" {
				g.Expect(res.ID).To(Equal(tt.wantID))
			}
			if tt.wantStatus != "" {
				g.Expect(res.Status).To(Equal(tt.wantStatus))
			}
		})
	}
}

func TestDeleteLB(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	lbHandler := mocks.NewMockLoadBalancersService(mockCtrl)
	collectionHandler := mocks.NewMockCollection[serverscom.LoadBalancer](mockCtrl)

	client := serverscom.NewClientWithEndpoint("", "")
	client.LoadBalancers = lbHandler
	manager := NewManager(client)

	label := "gw=uid"

	tests := []struct {
		name       string
		setupMocks func()
		wantErr    bool
	}{
		{
			name: "lb not found, ignore error",
			setupMocks: func() {
				lbHandler.EXPECT().
					Collection().
					Return(collectionHandler)
				collectionHandler.EXPECT().
					SetParam("type", "l7").
					Return(collectionHandler)
				collectionHandler.EXPECT().
					SetParam("label_selector", label).
					Return(collectionHandler)
				collectionHandler.EXPECT().
					Collect(gomock.Any()).
					Return(nil, &serverscom.NotFoundError{Message: "Not found"})
			},
			wantErr: false,
		},
		{
			name: "multiple lbs found",
			setupMocks: func() {
				lbHandler.EXPECT().
					Collection().
					Return(collectionHandler)
				collectionHandler.EXPECT().
					SetParam("type", "l7").
					Return(collectionHandler)
				collectionHandler.EXPECT().
					SetParam("label_selector", label).
					Return(collectionHandler)
				collectionHandler.EXPECT().
					Collect(gomock.Any()).
					Return([]serverscom.LoadBalancer{
						{ID: "lb1"}, {ID: "lb2"},
					}, nil)
			},
			wantErr: true,
		},
		{
			name: "delete single lb",
			setupMocks: func() {
				lbHandler.EXPECT().
					Collection().
					Return(collectionHandler)
				collectionHandler.EXPECT().
					SetParam("type", "l7").
					Return(collectionHandler)
				collectionHandler.EXPECT().
					SetParam("label_selector", label).
					Return(collectionHandler)
				collectionHandler.EXPECT().
					Collect(gomock.Any()).
					Return([]serverscom.LoadBalancer{
						{ID: "lb1"},
					}, nil)
				lbHandler.EXPECT().
					DeleteL7LoadBalancer(gomock.Any(), "lb1").
					Return(nil)
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			tt.setupMocks()
			err := manager.DeleteLB(context.Background(), label)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).To(BeNil())
			}
		})
	}
}

func TestTranslateGatewayToLBInput(t *testing.T) {
	g := NewWithT(t)

	certMap := map[string]string{
		"example.com": "cert-id",
	}

	tests := []struct {
		name      string
		gwInfo    *types.GatewayInfo
		hostCerts map[string]string
		wantErr   bool
		verify    func(lbInput *serverscom.L7LoadBalancerCreateInput)
	}{
		{
			name: "single vhost, single path, SSL on",
			gwInfo: &types.GatewayInfo{
				UID: "gw1",
				VHosts: map[string]*types.VHostInfo{
					"example.com": {
						Host:  "example.com",
						SSL:   true,
						Ports: []int32{443},
						Paths: []types.PathInfo{
							{
								Path: "/",
								Service: &corev1.Service{
									ObjectMeta: metav1.ObjectMeta{Name: "svc1"},
								},
								NodePort: 8080,
								NodeIps:  []string{"1.1.1.1"},
							},
						},
					},
				},
			},
			hostCerts: certMap,
			verify: func(lbInput *serverscom.L7LoadBalancerCreateInput) {
				g.Expect(len(lbInput.VHostZones)).To(Equal(1))
				vh := lbInput.VHostZones[0]
				g.Expect(vh.SSL).To(BeTrue())
				g.Expect(vh.SSLCertID).To(Equal("cert-id"))
				g.Expect(len(lbInput.UpstreamZones)).To(Equal(1))
			},
		},
		{
			name: "single vhost, multiple paths",
			gwInfo: &types.GatewayInfo{
				UID: "gw2",
				VHosts: map[string]*types.VHostInfo{
					"example.org": {
						Host:  "example.org",
						SSL:   false,
						Ports: []int32{80},
						Paths: []types.PathInfo{
							{
								Path: "/api",
								Service: &corev1.Service{
									ObjectMeta: metav1.ObjectMeta{Name: "svc2"},
								},
								NodePort: 8081,
								NodeIps:  []string{"2.2.2.2"},
							},
							{
								Path: "/web",
								Service: &corev1.Service{
									ObjectMeta: metav1.ObjectMeta{Name: "svc3"},
								},
								NodePort: 8082,
								NodeIps:  []string{"3.3.3.3"},
							},
						},
					},
				},
			},
			hostCerts: nil,
			verify: func(lbInput *serverscom.L7LoadBalancerCreateInput) {
				g.Expect(len(lbInput.VHostZones)).To(Equal(1))
				g.Expect(len(lbInput.VHostZones[0].LocationZones)).To(Equal(2))
				g.Expect(len(lbInput.UpstreamZones)).To(Equal(2))
			},
		},
		{
			name: "multiple vhosts",
			gwInfo: &types.GatewayInfo{
				UID: "gw3",
				VHosts: map[string]*types.VHostInfo{
					"a.com": {
						Host:  "a.com",
						SSL:   true,
						Ports: []int32{443},
						Paths: []types.PathInfo{
							{
								Path: "/",
								Service: &corev1.Service{
									ObjectMeta: metav1.ObjectMeta{Name: "svcA"},
								},
								NodePort: 8080,
								NodeIps:  []string{"1.1.1.1"},
							},
						},
					},
					"b.com": {
						Host:  "b.com",
						SSL:   false,
						Ports: []int32{80},
						Paths: []types.PathInfo{
							{
								Path: "/",
								Service: &corev1.Service{
									ObjectMeta: metav1.ObjectMeta{Name: "svcB"},
								},
								NodePort: 8081,
								NodeIps:  []string{"2.2.2.2"},
							},
						},
					},
				},
			},
			hostCerts: map[string]string{"a.com": "cert-a"},
			verify: func(lbInput *serverscom.L7LoadBalancerCreateInput) {
				g.Expect(len(lbInput.VHostZones)).To(Equal(2))
				for _, vh := range lbInput.VHostZones {
					if vh.Domains[0] == "a.com" {
						g.Expect(vh.SSLCertID).To(Equal("cert-a"))
					} else {
						g.Expect(vh.SSLCertID).To(Equal(""))
					}
				}
				g.Expect(len(lbInput.UpstreamZones)).To(Equal(2))
			},
		},
		{
			name: "empty ports/paths",
			gwInfo: &types.GatewayInfo{
				UID: "gw4",
				VHosts: map[string]*types.VHostInfo{
					"empty.com": {
						Host:  "empty.com",
						SSL:   false,
						Ports: []int32{},
						Paths: []types.PathInfo{},
					},
				},
			},
			hostCerts: nil,
			wantErr:   true,
		},
		{
			name: "SSL enabled but no cert in hostCerts",
			gwInfo: &types.GatewayInfo{
				UID: "gw5",
				VHosts: map[string]*types.VHostInfo{
					"nocert.com": {
						Host:  "nocert.com",
						SSL:   true,
						Ports: []int32{443},
						Paths: []types.PathInfo{
							{
								Path: "/",
								Service: &corev1.Service{
									ObjectMeta: metav1.ObjectMeta{Name: "svc"},
								},
								NodePort: 8080,
								NodeIps:  []string{"1.1.1.1"},
							},
						},
					},
				},
			},
			hostCerts: map[string]string{},
			verify: func(lbInput *serverscom.L7LoadBalancerCreateInput) {
				g.Expect(lbInput.VHostZones[0].SSL).To(BeTrue())
				g.Expect(lbInput.VHostZones[0].SSLCertID).To(Equal(""))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lbInput, err := translateGatewayToLBInput(tt.gwInfo, tt.hostCerts)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).To(BeNil())
			if tt.verify != nil {
				tt.verify(lbInput)
			}
			g.Expect(lbInput.Labels[config.GW_LABEL_ID]).To(Equal(tt.gwInfo.UID))
		})
	}
}
