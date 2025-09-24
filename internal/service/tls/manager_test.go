package tlssrv

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"math/big"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	serverscom "github.com/serverscom/serverscom-go-client/pkg"
	corev1 "k8s.io/api/core/v1"

	"github.com/serverscom/api-gateway-controller/internal/mocks"
	"github.com/serverscom/api-gateway-controller/internal/types"

	"go.uber.org/mock/gomock"
)

func TestEnsureTLS(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	sslHandler := mocks.NewMockSSLCertificatesService(mockCtrl)
	collectionHandler := mocks.NewMockCollection[serverscom.SSLCertificate](mockCtrl)

	sslHandler.EXPECT().
		Collection().
		Return(collectionHandler).
		AnyTimes()
	collectionHandler.EXPECT().
		SetParam(gomock.Any(), gomock.Any()).
		Return(collectionHandler).
		AnyTimes()

	client := serverscom.NewClientWithEndpoint("", "")
	client.SSLCertificates = sslHandler
	manager := NewManager(client)

	certPEM, keyPEM := generateCertAndKey(t)
	secret := &corev1.Secret{
		Data: map[string][]byte{
			corev1.TLSCertKey:       certPEM,
			corev1.TLSPrivateKeyKey: keyPEM,
		},
	}

	tests := []struct {
		name       string
		tlsInfo    map[string]types.TLSConfigInfo
		mock       func()
		wantErr    bool
		wantResult map[string]string
	}{
		{
			name: "external ID success",
			tlsInfo: map[string]types.TLSConfigInfo{
				"example.com": {ExternalID: "ext-id"},
			},
			mock: func() {
				sslHandler.EXPECT().
					GetCustom(gomock.Any(), "ext-id").
					Return(&serverscom.SSLCertificateCustom{ID: "cert-id"}, nil)
			},
			wantResult: map[string]string{"example.com": "cert-id"},
		},
		{
			name: "secret creates new cert",
			tlsInfo: map[string]types.TLSConfigInfo{
				"example.com": {Secret: secret},
			},
			mock: func() {
				collectionHandler.EXPECT().
					Collect(gomock.Any()).
					Return(nil, nil)
				sslHandler.EXPECT().
					CreateCustom(gomock.Any(), gomock.Any()).
					Return(&serverscom.SSLCertificateCustom{ID: "new-cert"}, nil)
			},
			wantResult: map[string]string{"example.com": "new-cert"},
		},
		{
			name: "secret missing key",
			tlsInfo: map[string]types.TLSConfigInfo{
				"example.com": {
					Secret: &corev1.Secret{
						Data: map[string][]byte{
							corev1.TLSCertKey: certPEM,
						},
					},
				},
			},
			mock:    func() {},
			wantErr: true,
		},
		{
			name: "external ID not found",
			tlsInfo: map[string]types.TLSConfigInfo{
				"example.com": {ExternalID: "not-found-id"},
			},
			mock: func() {
				sslHandler.EXPECT().
					GetCustom(gomock.Any(), "not-found-id").
					Return(nil, errors.New("not found"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			tt.mock()

			res, err := manager.EnsureTLS(context.Background(), tt.tlsInfo)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).To(BeNil())
			g.Expect(res).To(Equal(tt.wantResult))
		})
	}
}

func generateCertAndKey(t *testing.T) ([]byte, []byte) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"example.com"},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("failed to create cert: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("failed to marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	return certPEM, keyPEM
}
