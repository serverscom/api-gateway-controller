package lbsrv

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/serverscom/api-gateway-controller/internal/config"
	"github.com/serverscom/api-gateway-controller/internal/types"

	serverscom "github.com/serverscom/serverscom-go-client/pkg"
)

// translateGatewayToLBInput translates gateway based on gateway info and tlsInfo info into LB L7 create input
func translateGatewayToLBInput(gwInfo *types.GatewayInfo, tlsInfo map[string]string) (*serverscom.L7LoadBalancerCreateInput, error) {
	upstreamMap := make(map[string]serverscom.L7UpstreamZoneInput)
	var vhostZones []serverscom.L7VHostZoneInput

	for host, vh := range gwInfo.VHosts {
		sslEnabled := vh.SSL
		sslId := ""
		if sslEnabled {
			if id, ok := tlsInfo[host]; ok {
				sslId = id
			}
		}
		locationZones := []serverscom.L7LocationZoneInput{}
		for _, p := range vh.Paths {
			upstreamId := fmt.Sprintf("upstream-zone-%s-%d", p.Service.Name, p.NodePort)
			locationZones = append(locationZones, serverscom.L7LocationZoneInput{
				Location:   p.Path,
				UpstreamID: upstreamId,
			})
			if _, ok := upstreamMap[upstreamId]; !ok {
				var ups []serverscom.L7UpstreamInput
				for _, ip := range p.NodeIps {
					ups = append(ups, serverscom.L7UpstreamInput{
						IP:     ip,
						Port:   int32(p.NodePort),
						Weight: 1,
					})
				}
				upstreamMap[upstreamId] = serverscom.L7UpstreamZoneInput{
					ID:        upstreamId,
					Upstreams: ups,
				}
			}
		}
		if len(vh.Ports) == 0 || len(locationZones) == 0 {
			continue
		}
		vhostZones = append(vhostZones, serverscom.L7VHostZoneInput{
			ID:            fmt.Sprintf("vhost-zone-%s", host),
			Domains:       []string{host},
			SSLCertID:     sslId,
			SSL:           sslEnabled,
			Ports:         vh.Ports,
			LocationZones: locationZones,
		})
	}

	var upstreamZones []serverscom.L7UpstreamZoneInput
	for _, u := range upstreamMap {
		upstreamZones = append(upstreamZones, u)
	}
	if len(vhostZones) == 0 || len(upstreamZones) == 0 {
		return nil, fmt.Errorf("vhost or upstream can't be empty, can't continue")
	}
	locIdStr := config.FetchEnv("SC_LOCATION_ID", "1")
	locId, err := strconv.Atoi(locIdStr)
	if err != nil {
		locId = 1
	}
	lbInput := &serverscom.L7LoadBalancerCreateInput{
		Name:          getLoadBalancerName(gwInfo.UID),
		LocationID:    int64(locId),
		UpstreamZones: upstreamZones,
		VHostZones:    vhostZones,
		Labels: map[string]string{
			config.GW_LABEL_ID: gwInfo.UID,
		},
	}
	return lbInput, err
}

// GetLoadBalancerName compose a load balancer name from uid
func getLoadBalancerName(uid string) string {
	ret := "a" + uid
	ret = strings.Replace(ret, "-", "", -1)
	if len(ret) > 32 {
		ret = ret[:32]
	}
	return fmt.Sprintf("gw-%s", ret)
}
