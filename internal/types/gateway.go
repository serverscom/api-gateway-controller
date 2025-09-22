package types

import (
	corev1 "k8s.io/api/core/v1"
)

// GatewayInfo represents gateway info.
// Gathering in Reconcile loop contains info to build input for our load balancer.
type GatewayInfo struct {
	UID    string
	Name   string
	NS     string
	VHosts map[string]*VHostInfo
}

type PathInfo struct {
	Path     string
	Service  *corev1.Service
	NodePort int
	NodeIps  []string
}

type VHostInfo struct {
	Host  string
	SSL   bool
	Ports []int32
	Paths []PathInfo
}

// TLSConfigInfo represents tls info.
// Gathering in Reconcile loop contains info about tls config.
type TLSConfigInfo struct {
	ExternalID string
	Secret     *corev1.Secret
}

// ListenerInfo represents listener info.
// Used in Reconcile to filter available listeners.
type ListenerInfo struct {
	Name        string
	Hostname    string
	Protocol    string
	Port        int32
	AllowedFrom string
	Selector    map[string]string
}
