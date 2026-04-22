package desktopcore

import (
	"ben/registryauth"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestHTTPPeerLocatorAnnounceRetriesTransientBusy(t *testing.T) {
	t.Parallel()

	originalCount := registryAnnounceRetryCount
	originalDelay := registryAnnounceRetryDelay
	registryAnnounceRetryCount = 3
	registryAnnounceRetryDelay = 1
	t.Cleanup(func() {
		registryAnnounceRetryCount = originalCount
		registryAnnounceRetryDelay = originalDelay
	})

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/presence/announce" {
			http.NotFound(w, r)
			return
		}
		if attempts.Add(1) == 1 {
			http.Error(w, "authenticate presence announce: database is locked (5) (SQLITE_BUSY)", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	locator := newPeerLocator(server.URL)
	if locator == nil {
		t.Fatal("expected peer locator")
	}
	req := registryauth.PresenceAnnounceRequest{
		Record: registryauth.PresenceRecord{
			LibraryID: "library-1",
			DeviceID:  "device-1",
			PeerID:    "peer-1",
			Addrs:     []string{"/ip4/127.0.0.1/tcp/4101/p2p/peer-1"},
		},
		RootPublicKey: "root-key",
	}

	if err := locator.Announce(context.Background(), req); err != nil {
		t.Fatalf("announce with retry: %v", err)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("announce attempts = %d, want 2", got)
	}
}

func TestHTTPPeerLocatorAnnounceDoesNotRetryPermanentUnauthorized(t *testing.T) {
	t.Parallel()

	originalCount := registryAnnounceRetryCount
	originalDelay := registryAnnounceRetryDelay
	registryAnnounceRetryCount = 3
	registryAnnounceRetryDelay = 1
	t.Cleanup(func() {
		registryAnnounceRetryCount = originalCount
		registryAnnounceRetryDelay = originalDelay
	})

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		http.Error(w, "authenticate presence announce: invalid signature", http.StatusUnauthorized)
	}))
	defer server.Close()

	locator := newPeerLocator(server.URL)
	req := registryauth.PresenceAnnounceRequest{
		Record: registryauth.PresenceRecord{
			LibraryID: "library-1",
			DeviceID:  "device-1",
			PeerID:    "peer-1",
		},
		RootPublicKey: "root-key",
	}

	err := locator.Announce(context.Background(), req)
	if err == nil {
		t.Fatal("expected announce failure")
	}
	if !strings.Contains(err.Error(), "unexpected status 401") {
		t.Fatalf("announce error = %v, want 401", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("announce attempts = %d, want 1", got)
	}
}
