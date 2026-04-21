package desktopcore

import (
	"reflect"
	"testing"
)

func TestRelayBootstrapAddrsForHostMergesInviteAndAppConfig(t *testing.T) {
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
		"/ip4/198.51.100.12/tcp/4001/p2p/relay-invite",
		"/ip4/198.51.100.11/tcp/4001/p2p/relay-shared",
		"/ip4/198.51.100.10/tcp/4001/p2p/relay-config",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("relay bootstrap addrs = %#v, want %#v", got, want)
	}
}
