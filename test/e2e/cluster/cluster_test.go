package cluster

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
)

func TestWaitForAPIServerDNSResolves(t *testing.T) {
	err := waitForAPIServerDNS(context.Background(), logr.Discard(), "https://localhost", 10*time.Millisecond, 5*time.Second)
	if err != nil {
		t.Fatalf("expected localhost to resolve, got: %v", err)
	}
}

func TestWaitForAPIServerDNSTimesOut(t *testing.T) {
	// .invalid is reserved (RFC 6761) and never resolves.
	err := waitForAPIServerDNS(context.Background(), logr.Discard(), "https://cluster.eks.invalid", 10*time.Millisecond, 200*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "cluster.eks.invalid") {
		t.Fatalf("expected error to mention host, got: %v", err)
	}
}

func TestWaitForAPIServerDNSInvalidEndpoint(t *testing.T) {
	err := waitForAPIServerDNS(context.Background(), logr.Discard(), "not-a-url", 10*time.Millisecond, time.Second)
	if err == nil || !strings.Contains(err.Error(), "has no hostname") {
		t.Fatalf("expected no hostname error, got: %v", err)
	}
}
