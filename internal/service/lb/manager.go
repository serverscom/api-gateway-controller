package lbsrv

import (
	"context"
	"fmt"
	"strings"

	"github.com/serverscom/api-gateway-controller/internal/config"
	"github.com/serverscom/api-gateway-controller/internal/types"
	"github.com/serverscom/api-gateway-controller/internal/utils"

	serverscom "github.com/serverscom/serverscom-go-client/pkg"
)

//go:generate mockgen --destination ../../mocks/lb_manager.go --package=mocks --source manager.go

type LBManagerInterface interface {
	EnsureLB(ctx context.Context, gwInfo *types.GatewayInfo, hostCertMap map[string]string) (*serverscom.L7LoadBalancer, error)
	DeleteLB(ctx context.Context, labelSelector string) error
}

type Manager struct {
	scCli *serverscom.Client
}

func NewManager(c *serverscom.Client) *Manager {
	return &Manager{scCli: c}
}

// EnsureLB ensures a load balancer exists for the given GatewayInfo.
// It creates, updates, or returns existing LB status.
// hostCertMap contains external cert id for specific hosts.
func (s *Manager) EnsureLB(ctx context.Context, gwInfo *types.GatewayInfo, hostCertMap map[string]string) (*serverscom.L7LoadBalancer, error) {
	labelSelector := config.GW_LABEL_ID + "=" + gwInfo.UID
	lbs, err := s.getL7LoadBalancersByLabel(ctx, labelSelector)
	if err != nil {
		return nil, err
	}
	if len(lbs) == 0 {
		// create lb
		lbInput, err := translateGatewayToLBInput(gwInfo, hostCertMap)
		if err != nil {
			return nil, err
		}
		return s.scCli.LoadBalancers.CreateL7LoadBalancer(ctx, *lbInput)
	}
	if len(lbs) > 1 {
		return nil, fmt.Errorf("found more than one lb with same label")
	}
	// if not active yet, just return status to reconcile again
	lb := lbs[0]
	if !strings.EqualFold(lb.Status, config.LB_ACTIVE_STATUS) {
		lbl7 := &serverscom.L7LoadBalancer{
			Status: lb.Status,
		}
		return lbl7, nil
	}
	// update lb
	lbInput, err := translateGatewayToLBInput(gwInfo, hostCertMap)
	if err != nil {
		return nil, err
	}

	lbUpdateInput := serverscom.L7LoadBalancerUpdateInput{
		Name:              lbInput.Name,
		StoreLogs:         lbInput.StoreLogs,
		StoreLogsRegionID: lbInput.StoreLogsRegionID,
		Geoip:             lbInput.Geoip,
		VHostZones:        lbInput.VHostZones,
		UpstreamZones:     lbInput.UpstreamZones,
		ClusterID:         lbInput.ClusterID,
	}
	if lbUpdateInput.ClusterID == nil {
		lbUpdateInput.SharedCluster = utils.BoolPtr(true)
	}

	return s.scCli.LoadBalancers.UpdateL7LoadBalancer(ctx, lb.ID, lbUpdateInput)
}

// DeleteLB deletes a load balancer by its label selector.
// Returns error if multiple LBs are found.
func (s *Manager) DeleteLB(ctx context.Context, labelSelector string) error {
	lbs, err := s.getL7LoadBalancersByLabel(ctx, labelSelector)
	if err != nil {
		return utils.IgnoreNotFound(err)
	}
	if len(lbs) > 1 {
		return fmt.Errorf("found more than one lb with same label")
	}
	return s.scCli.LoadBalancers.DeleteL7LoadBalancer(ctx, lbs[0].ID)
}

// getL7LoadBalancersByLabel retrieves all L7 load balancers from provider filtered by label selector.
func (s *Manager) getL7LoadBalancersByLabel(ctx context.Context, labelSelector string) ([]serverscom.LoadBalancer, error) {
	return s.scCli.LoadBalancers.Collection().
		SetParam("type", "l7").
		SetParam("label_selector", labelSelector).
		Collect(ctx)
}
