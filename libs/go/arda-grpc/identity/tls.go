package identity

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"

	"google.golang.org/grpc/credentials"
)

const (
	caFileEnv   = "ARDA_GRPC_CA_FILE"
	certFileEnv = "ARDA_GRPC_CERT_FILE"
	keyFileEnv  = "ARDA_GRPC_KEY_FILE"
)

// ClientTransportCredentials loads the workload's client certificate and CA
// from mounted secret files. There is intentionally no plaintext fallback.
func ClientTransportCredentials(serverName string) (credentials.TransportCredentials, error) {
	caPool, cert, err := loadTLSMaterial()
	if err != nil {
		return nil, err
	}
	serverName = strings.TrimSpace(serverName)
	if serverName == "" {
		return nil, fmt.Errorf("grpc tls server name is required")
	}
	return credentials.NewTLS(&tls.Config{
		MinVersion:   tls.VersionTLS13,
		RootCAs:      caPool,
		Certificates: []tls.Certificate{cert},
		ServerName:   serverName,
		// Client cert authentication is enforced by the destination server.
		InsecureSkipVerify: false,
	}), nil
}

// ServerTransportCredentials loads the service certificate and requires every
// gRPC client to present a certificate signed by the configured CA.
func ServerTransportCredentials() (credentials.TransportCredentials, error) {
	caPool, cert, err := loadTLSMaterial()
	if err != nil {
		return nil, err
	}
	return credentials.NewTLS(&tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
		ClientCAs:    caPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}), nil
}

func loadTLSMaterial() (*x509.CertPool, tls.Certificate, error) {
	caFile := strings.TrimSpace(os.Getenv(caFileEnv))
	certFile := strings.TrimSpace(os.Getenv(certFileEnv))
	keyFile := strings.TrimSpace(os.Getenv(keyFileEnv))
	if caFile == "" || certFile == "" || keyFile == "" {
		return nil, tls.Certificate{}, fmt.Errorf("grpc tls requires %s, %s and %s", caFileEnv, certFileEnv, keyFileEnv)
	}
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, tls.Certificate{}, fmt.Errorf("read grpc tls ca: %w", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		return nil, tls.Certificate{}, fmt.Errorf("parse grpc tls ca: no certificates found")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, tls.Certificate{}, fmt.Errorf("load grpc tls certificate: %w", err)
	}
	return caPool, cert, nil
}
