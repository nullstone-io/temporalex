package temporalex

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"go.temporal.io/sdk/client"
	oteltemporal "go.temporal.io/sdk/contrib/opentelemetry"
	"go.temporal.io/sdk/interceptor"
	"os"
)

const (
	TemporalHostPortEnvVar  = "TEMPORAL_HOSTPORT"
	TemporalNamespaceEnvVar = "TEMPORAL_NAMESPACE"
	TemporalTlsCert         = "TEMPORAL_TLS_CERT"
	TemporalTlsKey          = "TEMPORAL_TLS_KEY"
)

func DefaultClient() (client.Client, error) {
	opts := client.Options{
		HostPort:          client.DefaultHostPort,
		Namespace:         os.Getenv(TemporalNamespaceEnvVar),
		ConnectionOptions: client.ConnectionOptions{},
		Interceptors:      []interceptor.ClientInterceptor{},
	}
	if val := os.Getenv(TemporalHostPortEnvVar); val != "" {
		opts.HostPort = val
	}
	tlsCert, err := tlsCertificateFromEnv()
	if err != nil {
		return nil, fmt.Errorf("error initializing TLS certificate for temporal client: %w", err)
	}
	if tlsCert != nil {
		opts.ConnectionOptions.TLS = &tls.Config{Certificates: []tls.Certificate{*tlsCert}}
	}
	tracingInterceptor, err := oteltemporal.NewTracingInterceptor(oteltemporal.TracerOptions{})
	if err != nil {
		return nil, fmt.Errorf("error initializing opentelemetry tracing interceptor for temporal client: %w", err)
	}
	opts.Interceptors = append(opts.Interceptors, tracingInterceptor)
	return client.Dial(opts)
}

func tlsCertificateFromEnv() (*tls.Certificate, error) {
	tlsCert, tlsKey := os.Getenv(TemporalTlsCert), os.Getenv(TemporalTlsKey)
	if tlsCert == "" {
		return nil, nil
	}

	certPem, err := base64.StdEncoding.DecodeString(tlsCert)
	if err != nil {
		return nil, fmt.Errorf("unable to decode base-64 tls certificate: %w", err)
	}

	keyPem, err := base64.StdEncoding.DecodeString(tlsKey)
	if err != nil {
		return nil, fmt.Errorf("unable to decode base-64 tls key: %w", err)
	}

	cert, err := tls.X509KeyPair(certPem, keyPem)
	if err != nil {
		return nil, fmt.Errorf("error creating x509 keypair: %w", err)
	}
	return &cert, nil
}
