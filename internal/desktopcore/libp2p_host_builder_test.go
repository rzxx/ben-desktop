package desktopcore

import (
	"reflect"
	"testing"
)

func TestRelayBootstrapAddrsForHostPrefersAppConfig(t *testing.T) {
	t.Parallel()

	app := &App{
		cfg: Config{
			RelayBootstrapAddrs: []string{
				"/ip4/198.51.100.10/tcp/4001/p2p/relay-config",
				"/ip4/198.51.100.11/tcp/4001/p2p/relay-shared",
			},
		},
	}

	got := app.relayBootstrapAddrsForHost([]string{
		"/ip4/198.51.100.12/tcp/4001/p2p/relay-invite",
		"/ip4/198.51.100.11/tcp/4001/p2p/relay-shared",
	})
	want := []string{
		"/ip4/198.51.100.10/tcp/4001/p2p/relay-config",
		"/ip4/198.51.100.11/tcp/4001/p2p/relay-shared",
		"/ip4/198.51.100.12/tcp/4001/p2p/relay-invite",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("relay bootstrap addrs = %#v, want %#v", got, want)
	}
}

func TestPeerLocatorPrefersConfiguredRegistryURL(t *testing.T) {
	t.Parallel()

	app := &App{
		cfg: Config{RegistryURL: "https://configured-relay.example"},
	}

	locator := app.peerLocator("https://invite-relay.example")
	httpLocator, ok := locator.(*httpPeerLocator)
	if !ok {
		t.Fatalf("peer locator type = %T", locator)
	}
	if httpLocator.baseURL != "https://configured-relay.example" {
		t.Fatalf("peer locator base url = %q", httpLocator.baseURL)
	}
}

func TestPeerLocatorFallsBackToInviteRegistryURL(t *testing.T) {
	t.Parallel()

	app := &App{}

	locator := app.peerLocator("https://invite-relay.example")
	httpLocator, ok := locator.(*httpPeerLocator)
	if !ok {
		t.Fatalf("peer locator type = %T", locator)
	}
	if httpLocator.baseURL != "https://invite-relay.example" {
		t.Fatalf("peer locator base url = %q", httpLocator.baseURL)
	}
}
