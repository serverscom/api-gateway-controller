package tlssrv

import (
	"crypto/sha1"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"

	serverscom "github.com/serverscom/serverscom-go-client/pkg"
)

// CustomToSSLCertificate converts a serverscom SSLCertificateCustom to serverscom SSLCertificate
func customToSSLCertificate(custom *serverscom.SSLCertificateCustom) *serverscom.SSLCertificate {
	return &serverscom.SSLCertificate{
		ID:              custom.ID,
		Name:            custom.Name,
		Sha1Fingerprint: custom.Sha1Fingerprint,
		Labels:          custom.Labels,
		Expires:         custom.Expires,
		Created:         custom.Created,
		Updated:         custom.Updated,
	}
}

// GetPemFingerprint returns sha1 fingerprint from cert
func getPemFingerprint(crt []byte) string {
	cert := findCertificate(stripSpaces(crt))

	if cert != nil {
		sha := sha1.Sum(cert)
		return fmt.Sprintf("%x", sha)
	} else {
		return ""
	}
}

// ValidateCertificate validates that certificate is valid
func validateCertificate(crt []byte) error {
	primary, _ := splitCerts(crt)

	if primary == nil {
		return fmt.Errorf("can't find certificate, please verify your tls.crt section")
	}

	certDERBlock, _ := pem.Decode(primary)
	if certDERBlock == nil {
		return fmt.Errorf("can't find certificate, please verify your tls.crt section")
	}

	if certDERBlock.Type != "CERTIFICATE" {
		return fmt.Errorf("can't find certificate, expected CERTIFICATE, got: %s", certDERBlock.Type)
	}

	cert, err := x509.ParseCertificate(certDERBlock.Bytes)
	if err != nil {
		return fmt.Errorf("can't parse certificate: %s", err.Error())
	}

	if len(cert.DNSNames) == 0 {
		return fmt.Errorf("can't find dns names for certificate")
	}

	return nil
}

// FindCertificate finds DER block from cert
func findCertificate(crt []byte) []byte {
	certDERBlock, _ := pem.Decode(crt)
	if certDERBlock == nil {
		return nil
	}

	if certDERBlock.Type == "CERTIFICATE" {
		return certDERBlock.Bytes
	}

	return nil
}

// SplitCerts splits cert with bundle to cert and bundle
func splitCerts(crt []byte) ([]byte, []byte) {
	var sanitizedCert = string(stripSpaces(crt))
	var primary []string
	var chain []string

	var started = false
	var iter = 0

	for _, line := range strings.Split(sanitizedCert, "\n") {
		if strings.HasPrefix(line, "-----BEGIN") {
			started = true
		}

		if !started {
			continue
		}

		if iter == 0 {
			primary = append(primary, strings.TrimSpace(line))
		} else {
			chain = append(chain, strings.TrimSpace(line))
		}

		if strings.HasPrefix(line, "-----END") {
			started = false
			iter = iter + 1
		}
	}

	if len(primary) == 0 {
		return nil, nil
	}

	if len(chain) == 0 {
		return []byte(strings.Join(primary, "\n")), nil
	}

	return []byte(strings.Join(primary, "\n")), []byte(strings.Join(chain, "\n"))
}

// StripSpaces removes strip spaces from str
func stripSpaces(b []byte) []byte {
	var out []string
	for line := range strings.SplitSeq(string(b), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return []byte(strings.Join(out, "\n"))
}
