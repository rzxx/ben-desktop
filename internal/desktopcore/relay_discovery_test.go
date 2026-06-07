package desktopcore

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

const testRelayPeerID = "12D3KooWCp9LMFppvKSVRMKv4mSHaji7TXENLgcGzUNfiAgfqKcb"

func TestRelayBootstrapAddrsFromHealthAppendsPeerID(t *testing.T) {
	t.Parallel()

	got := relayBootstrapAddrsFromHealth(relayHealthResponse{
		PeerID: testRelayPeerID,
		Addrs: []string{
			"/dns4/ben-project-production-rzx.unkey.app/tcp/443/wss",
			"not-a-multiaddr",
		},
	})
	want := []string{
		"/dns4/ben-project-production-rzx.unkey.app/tcp/443/wss/p2p/" + testRelayPeerID,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("relay bootstrap addrs = %#v, want %#v", got, want)
	}
}

func TestRelayBootstrapAddrsForHostPrefersDiscoveredRegistryRelay(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Fatalf("request path = %q", r.URL.Path)
		}
		writeTestJSON(t, w, relayHealthResponse{
			Addrs: []string{"/dns4/discovered.example/tcp/443/wss/p2p/" + testRelayPeerID},
		})
	}))
	defer server.Close()

	app := &App{
		cfg: Config{
			RegistryURL: server.URL,
			RelayBootstrapAddrs: []string{
				"/dns4/stale.example/tcp/443/wss/p2p/" + testRelayPeerID,
			},
		},
	}

	got := app.relayBootstrapAddrsForHost([]string{
		"/dns4/invite.example/tcp/443/wss/p2p/" + testRelayPeerID,
	})
	want := []string{
		"/dns4/discovered.example/tcp/443/wss/p2p/" + testRelayPeerID,
		"/dns4/stale.example/tcp/443/wss/p2p/" + testRelayPeerID,
		"/dns4/invite.example/tcp/443/wss/p2p/" + testRelayPeerID,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("relay bootstrap addrs = %#v, want %#v", got, want)
	}
}

func writeTestJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
