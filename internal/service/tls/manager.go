package tlssrv

import (
	"context"
	"fmt"

	"github.com/serverscom/api-gateway-controller/internal/config"
	"github.com/serverscom/api-gateway-controller/internal/types"
	"github.com/serverscom/api-gateway-controller/internal/utils"

	corev1 "k8s.io/api/core/v1"

	serverscom "github.com/serverscom/serverscom-go-client/pkg"
)

//go:generate mockgen --destination ../../mocks/tls_manager.go --package=mocks --source manager.go

type TLSManagerInterface interface {
	EnsureTLS(ctx context.Context, tlsInfo map[string]types.TLSConfigInfo) (map[string]string, error)
}

type Manager struct {
	scCli *serverscom.Client
}

func NewManager(c *serverscom.Client) *Manager {
	return &Manager{scCli: c}
}

// EnsureTLS ensures all TLS certificates exist in the provider.
// It supports either a secret or an external certificate ID for each host.
// External ID overrides cert from secret.
// Returns a map of host to certificate external ID.
func (m *Manager) EnsureTLS(ctx context.Context, tlsInfo map[string]types.TLSConfigInfo) (map[string]string, error) {
	res := make(map[string]string)
	for host, info := range tlsInfo {
		if info.ExternalID != "" {
			cert, err := m.getByID(info.ExternalID)
			if err != nil {
				return nil, fmt.Errorf("provider certificate id %q for host %q not found: %w", info.ExternalID, host, err)
			}
			res[host] = cert.ID
			continue
		}
		secret := info.Secret
		if secret == nil {
			return nil, fmt.Errorf("no secret or ExternalID for host %q", host)
		}
		certPEM, ok := secret.Data[corev1.TLSCertKey]
		if !ok {
			return nil, fmt.Errorf("secret for host %q has no tls.crt", host)
		}
		keyPEM, ok := secret.Data[corev1.TLSPrivateKeyKey]
		if !ok {
			return nil, fmt.Errorf("secret for host %q has no tls.key", host)
		}
		if err := validateCertificate(certPEM); err != nil {
			return nil, fmt.Errorf("invalid certificate for host %q: %w", host, err)
		}
		primary, chain := splitCerts(certPEM)
		fp := getPemFingerprint(primary)
		certObj, err := m.ensureCertificateForSecret(ctx, fp, string(secret.UID), primary, keyPEM, chain)
		if err != nil {
			return nil, fmt.Errorf("findOrCreate tls for host %q failed: %w", host, err)
		}
		res[host] = certObj.ID
	}
	return res, nil
}

// getByID gets cert by external ID
func (m *Manager) getByID(id string) (*serverscom.SSLCertificate, error) {
	customCert, err := m.scCli.SSLCertificates.GetCustom(context.Background(), id)
	if err != nil {
		return nil, err
	}

	return customToSSLCertificate(customCert), nil
}

// ensureCertificateForSecret ensures a certificate exists for a given secret.
// It finds, updates, or creates the certificate as needed.
func (m *Manager) ensureCertificateForSecret(
	ctx context.Context,
	fingerprint, secretUID string,
	cert, key, chain []byte,
) (*serverscom.SSLCertificate, error) {
	foundCrt, err := m.findCertificate(ctx, fingerprint, secretUID)
	if err != nil {
		return nil, err
	}
	if foundCrt != nil && foundCrt.Sha1Fingerprint == fingerprint {
		return foundCrt, nil
	}
	if foundCrt != nil && foundCrt.ID != "" {
		return m.updateCertificateForSecret(ctx, foundCrt.ID, cert, key, chain)
	}
	return m.createCertificateForSecret(ctx, secretUID, cert, key, chain)
}

// findCertificate searches for a certificate in provider by secret label.
// fingerprint is used to match same cert.
func (m *Manager) findCertificate(ctx context.Context, fingerprint, secretUID string) (*serverscom.SSLCertificate, error) {
	labelSelector := config.SECRET_LABEL_ID + "=" + secretUID
	certs, err := m.scCli.SSLCertificates.Collection().
		SetParam("label_selector", labelSelector).
		SetParam("type", "custom").
		Collect(ctx)
	if err != nil {
		return nil, utils.IgnoreNotFound(err)
	}
	for _, c := range certs {
		if c.Sha1Fingerprint == fingerprint {
			return &c, nil
		}
	}
	if len(certs) > 0 {
		// Return first for update use
		return &certs[0], nil
	}
	return nil, nil
}

// updateCertificateForSecret updates certificate in provider.
func (m *Manager) updateCertificateForSecret(ctx context.Context, id string, cert, key, chain []byte) (*serverscom.SSLCertificate, error) {
	in := serverscom.SSLCertificateUpdateCustomInput{
		PublicKey:  string(cert),
		PrivateKey: string(key),
	}
	if len(chain) > 0 {
		in.ChainKey = string(chain)
	}
	out, err := m.scCli.SSLCertificates.UpdateCustom(ctx, id, in)
	if err != nil {
		return nil, err
	}
	return customToSSLCertificate(out), nil
}

// createCertificateForSecret creates a new certificate in provider.
func (m *Manager) createCertificateForSecret(ctx context.Context, secretUID string, cert, key, chain []byte) (*serverscom.SSLCertificate, error) {
	in := serverscom.SSLCertificateCreateCustomInput{
		Name:       "gw-secret-" + secretUID,
		PublicKey:  string(cert),
		PrivateKey: string(key),
		Labels: map[string]string{
			config.SECRET_LABEL_ID: secretUID,
		},
	}
	if len(chain) > 0 {
		in.ChainKey = string(chain)
	}
	out, err := m.scCli.SSLCertificates.CreateCustom(ctx, in)
	if err != nil {
		return nil, err
	}
	return customToSSLCertificate(out), nil
}
