package tests

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"os"
	"testing"
	"time"

	"github.com/thebadinteger/rtsnap"
	"github.com/thebadinteger/rtsnap/pkg/h264"
)

func selfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "rtsnap-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:         true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func TestSnapshotRTSPSDefaultTLS(t *testing.T) {
	data, err := os.ReadFile("testdata/black_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}
	server, err := newMockServer(&mockServer{
		codec:   "h264",
		packets: makeRTPPacketsH264(h264.ExtractNalusFromByteStream(data)),
	})
	if err != nil {
		t.Fatalf("mock server: %v", err)
	}
	defer server.Close()

	cert := selfSignedCert(t)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatalf("tls listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go server.handle(conn)
		}
	}()

	url := fmt.Sprintf("rtsps://%s/live", ln.Addr())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	img, err := rtsnap.Snapshot(ctx, url)
	if err != nil {
		t.Fatalf("rtsps snapshot with default tls: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil image")
	}
}

func TestSnapshotRTSPSVerifyRejectsSelfSigned(t *testing.T) {
	data, err := os.ReadFile("testdata/black_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}
	server, err := newMockServer(&mockServer{
		codec:   "h264",
		packets: makeRTPPacketsH264(h264.ExtractNalusFromByteStream(data)),
	})
	if err != nil {
		t.Fatalf("mock server: %v", err)
	}
	defer server.Close()

	cert := selfSignedCert(t)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatalf("tls listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go server.handle(conn)
		}
	}()

	url := fmt.Sprintf("rtsps://%s/live", ln.Addr())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := rtsnap.Snapshot(ctx, url, rtsnap.WithTLSVerify()); err == nil {
		t.Fatal("expected certificate verification failure")
	}
}
