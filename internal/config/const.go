package config

const (
	GW_DOMAIN               = "k8s.srvrscloud.com"
	DEFAULT_GATEWAY_CLASS   = "" // all
	DEFAULT_CONTROLLER_NAME = GW_DOMAIN + "/gateway-controller"
	GW_FINALIZER            = GW_DOMAIN + "/gateway-cleanup"
	GW_LABEL_ID             = GW_DOMAIN + "/api-gateway-id"
	SECRET_LABEL_ID         = GW_DOMAIN + "/api-secret-id"
	TLS_EXTERNAL_ID_KEY     = "sc-certmgr-cert-id"

	SC_API_URL = "https://api.servers.com/v1"

	LB_ACTIVE_STATUS = "active"
)
