package checks

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/noklash/netprobe/internal/diagnostic"
)

type tlsFixture struct {
	address netip.Addr
	port    int
	roots   *x509.CertPool
}

func startTLSFixture(
	t *testing.T,
	dnsName string,
	performHandshake bool,
) tlsFixture {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate private key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: dnsName,
		},
		DNSNames:              []string{dnsName},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(
		rand.Reader,
		template,
		template,
		&privateKey.PublicKey,
		privateKey,
	)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("load key pair: %v", err)
	}

	parsedCert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}

	roots := x509.NewCertPool()
	roots.AddCert(parsedCert)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	t.Cleanup(func() {
		_ = listener.Close()
	})

	serverConn := make(chan net.Conn, 1)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}

		serverConn <- conn

		if performHandshake {
			server := tls.Server(conn, &tls.Config{
				Certificates: []tls.Certificate{certificate},
				MinVersion:   tls.VersionTLS12,
			})
			_ = server.Handshake()
		}

		_ = conn.Close()
	}()

	t.Cleanup(func() {
		select {
		case conn := <-serverConn:
			_ = conn.Close()
		default:
		}
	})

	host, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split listener address: %v", err)
	}

	address, err := netip.ParseAddr(host)
	if err != nil {
		t.Fatalf("parse listener IP: %v", err)
	}

	var port int
	for _, digit := range portText {
		port = port*10 + int(digit-'0')
	}

	return tlsFixture{
		address: address,
		port:    port,
		roots:   roots,
	}
}

func TestCheckTLSSuccess(t *testing.T) {
	fixture := startTLSFixture(t, "example.test", true)

	result, err := CheckTLS(
		context.Background(),
		&net.Dialer{},
		fixture.address,
		"example.test",
		fixture.port,
		time.Second,
		fixture.roots,
	)
	if err != nil {
		t.Fatalf("CheckTLS() error = %v", err)
	}

	if result.Status != diagnostic.StatusPassed {
		t.Fatalf("status = %q, want passed; error: %s",
			result.Status, result.Error)
	}

	if result.Name != "tls" {
		t.Errorf("name = %q, want tls", result.Name)
	}

	if result.Details["tls_version"] != "1.3" &&
		result.Details["tls_version"] != "1.2" {
		t.Errorf("unexpected TLS version: %v", result.Details["tls_version"])
	}

	if result.Details["verified_chains"] == 0 {
		t.Error("expected at least one verified certificate chain")
	}

	if err := result.Validate(); err != nil {
		t.Fatalf("result validation failed: %v", err)
	}
}

func TestCheckTLSRejectsHostnameMismatch(t *testing.T) {
	fixture := startTLSFixture(t, "example.test", true)

	result, err := CheckTLS(
		context.Background(),
		&net.Dialer{},
		fixture.address,
		"wrong.example.test",
		fixture.port,
		time.Second,
		fixture.roots,
	)
	if err != nil {
		t.Fatalf("CheckTLS() error = %v", err)
	}

	if result.Status != diagnostic.StatusFailed {
		t.Fatalf("status = %q, want failed", result.Status)
	}

	if result.Error == "" {
		t.Error("expected certificate verification error")
	}
}

func TestCheckTLSRejectsUntrustedCertificate(t *testing.T) {
	fixture := startTLSFixture(t, "example.test", true)

	result, err := CheckTLS(
		context.Background(),
		&net.Dialer{},
		fixture.address,
		"example.test",
		fixture.port,
		time.Second,
		nil,
	)
	if err != nil {
		t.Fatalf("CheckTLS() error = %v", err)
	}

	if result.Status != diagnostic.StatusFailed {
		t.Fatalf("status = %q, want failed", result.Status)
	}

	if result.Error == "" {
		t.Error("expected certificate trust error")
	}
}

func TestCheckTLSConnectionFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve local port: %v", err)
	}

	host, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		_ = listener.Close()
		t.Fatalf("split listener address: %v", err)
	}

	address := netip.MustParseAddr(host)
	_ = listener.Close()

	var port int
	for _, digit := range portText {
		port = port*10 + int(digit-'0')
	}

	result, err := CheckTLS(
		context.Background(),
		&net.Dialer{},
		address,
		"example.test",
		port,
		time.Second,
		nil,
	)
	if err != nil {
		t.Fatalf("CheckTLS() error = %v", err)
	}

	if result.Status != diagnostic.StatusFailed {
		t.Fatalf("status = %q, want failed", result.Status)
	}
}

type pipeDialer struct {
	peer net.Conn
}

func (d *pipeDialer) DialContext(
	_ context.Context,
	_ string,
	_ string,
) (net.Conn, error) {
	client, peer := net.Pipe()
	d.peer = peer
	return client, nil
}

func TestCheckTLSTimeoutIsInconclusive(t *testing.T) {
	dialer := &pipeDialer{}

	t.Cleanup(func() {
		if dialer.peer != nil {
			_ = dialer.peer.Close()
		}
	})

	result, err := CheckTLS(
		context.Background(),
		dialer,
		netip.MustParseAddr("192.0.2.10"),
		"example.test",
		443,
		30*time.Millisecond,
		nil,
	)
	if err != nil {
		t.Fatalf("CheckTLS() error = %v", err)
	}

	if result.Status != diagnostic.StatusInconclusive {
		t.Fatalf("status = %q, want inconclusive", result.Status)
	}

	if result.Error == "" {
		t.Error("expected timeout or cancellation error")
	}
}

func TestCheckTLSRejectsInvalidConfiguration(t *testing.T) {
	validContext := context.Background()
	validDialer := &net.Dialer{}
	validAddress := netip.MustParseAddr("192.0.2.10")

	tests := []struct {
		name     string
		ctx      context.Context
		dialer   Dialer
		address  netip.Addr
		hostname string
		port     int
		timeout  time.Duration
	}{
		{
			name:     "nil context",
			ctx:      nil,
			dialer:   validDialer,
			address:  validAddress,
			hostname: "example.test",
			port:     443,
			timeout:  time.Second,
		},
		{
			name:     "nil dialer",
			ctx:      validContext,
			dialer:   nil,
			address:  validAddress,
			hostname: "example.test",
			port:     443,
			timeout:  time.Second,
		},
		{
			name:     "invalid IP",
			ctx:      validContext,
			dialer:   validDialer,
			address:  netip.Addr{},
			hostname: "example.test",
			port:     443,
			timeout:  time.Second,
		},
		{
			name:     "empty hostname",
			ctx:      validContext,
			dialer:   validDialer,
			address:  validAddress,
			hostname: "",
			port:     443,
			timeout:  time.Second,
		},
		{
			name:     "invalid port",
			ctx:      validContext,
			dialer:   validDialer,
			address:  validAddress,
			hostname: "example.test",
			port:     65536,
			timeout:  time.Second,
		},
		{
			name:     "zero timeout",
			ctx:      validContext,
			dialer:   validDialer,
			address:  validAddress,
			hostname: "example.test",
			port:     443,
			timeout:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CheckTLS(
				tt.ctx,
				tt.dialer,
				tt.address,
				tt.hostname,
				tt.port,
				tt.timeout,
				nil,
			)
			if err == nil {
				t.Fatal("CheckTLS() error = nil, want configuration error")
			}
		})
	}
}
