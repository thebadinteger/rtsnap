package tests

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/thebadinteger/rtsnap"
	"github.com/thebadinteger/rtsnap/pkg/h264"
	"github.com/thebadinteger/rtsnap/pkg/rtsp"
)

func TestAuthInvalidCredentials(t *testing.T) {
	data, err := os.ReadFile("testdata/black_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}

	nalus := h264.ExtractNalusFromByteStream(data)
	server, err := newMockServer(&mockServer{
		authMode: "digest",
		username: "realuser",
		password: "realpassword",
		codec:    "h264",
		packets:  makeRTPPacketsH264(nalus),
	})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = rtsnap.Snapshot(ctx, server.URL(), rtsnap.WithAuth("realuser", "wrongpassword"))
	if err == nil {
		t.Fatal("expected error with invalid credentials, got nil")
	}
	if !strings.Contains(err.Error(), "401") && !strings.Contains(err.Error(), "rejected") {
		t.Logf("got error: %v", err)
	}
}

func TestAuthDigestSHA256(t *testing.T) {
	data, err := os.ReadFile("testdata/black_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}

	nalus := h264.ExtractNalusFromByteStream(data)
	server, err := newMockServer(&mockServer{
		authMode: "digest-sha256",
		username: "shauser",
		password: "shabigsecret",
		codec:    "h264",
		packets:  makeRTPPacketsH264(nalus),
	})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	img, err := rtsnap.Snapshot(ctx, server.URL(), rtsnap.WithAuth("shauser", "shabigsecret"))
	if err != nil {
		t.Fatalf("sha256 digest auth failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil image")
	}
}

func TestAuthURLCredentials(t *testing.T) {
	data, err := os.ReadFile("testdata/black_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}

	nalus := h264.ExtractNalusFromByteStream(data)
	server, err := newMockServer(&mockServer{
		authMode: "basic",
		username: "urluser",
		password: "urlpassword",
		codec:    "h264",
		packets:  makeRTPPacketsH264(nalus),
	})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	u := strings.Replace(server.URL(), "rtsp://", "rtsp://urluser:urlpassword@", 1)
	img, err := rtsnap.Snapshot(ctx, u)
	if err != nil {
		t.Fatalf("snapshot with url credentials failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil image")
	}
}

func TestAuthLateOnSetup(t *testing.T) {
	data, err := os.ReadFile("testdata/black_idr.264")
	if err != nil {
		t.Skip("testdata not found")
	}

	nalus := h264.ExtractNalusFromByteStream(data)
	server, err := newMockServer(&mockServer{
		authMode: "digest",
		username: "lateuser",
		password: "latepassword",
		authLate: true,
		codec:    "h264",
		packets:  makeRTPPacketsH264(nalus),
	})
	if err != nil {
		t.Fatalf("newMockServer: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	img, err := rtsnap.Snapshot(ctx, server.URL(), rtsnap.WithAuth("lateuser", "latepassword"))
	if err != nil {
		t.Fatalf("late auth snapshot failed: %v", err)
	}
	if img == nil {
		t.Fatal("expected non-nil image")
	}
}

func TestAuthUnitHeaders(t *testing.T) {
	hdr := `Digest realm="myrealm", nonce="abcd1234"`
	a := rtsp.NewAuth(hdr, "user1", "pass1")
	if a == nil {
		t.Fatal("failed to create auth")
	}
	res := a.Generate("DESCRIBE", "rtsp://127.0.0.1/live")
	if !strings.Contains(res, `realm="myrealm"`) || !strings.Contains(res, `nonce="abcd1234"`) {
		t.Fatalf("malformed response: %s", res)
	}

	badAuth := rtsp.NewAuth(`NTLM realm="windows"`, "u", "p")
	if badAuth != nil {
		t.Fatalf("expected nil for unsupported auth scheme, got %v", badAuth)
	}
}
