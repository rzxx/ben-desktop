package desktopcore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

const relayDiscoveryTimeout = 3 * time.Second

type relayHealthResponse struct {
	Addrs  []string `json:"addrs"`
	PeerID string   `json:"peerId"`
}

func (a *App) discoverRelayBootstrapAddrsFromRegistry() []string {
	if a == nil {
		return nil
	}
	registryURL := strings.TrimSpace(a.cfg.RegistryURL)
	if registryURL == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), relayDiscoveryTimeout)
	defer cancel()
	addrs, err := fetchRelayBootstrapAddrs(ctx, registryURL)
	if err != nil {
		a.logf("desktopcore: relay discovery from %s failed: %v", registryURL, err)
		return nil
	}
	return addrs
}

func fetchRelayBootstrapAddrs(ctx context.Context, registryURL string) ([]string, error) {
	registryURL = strings.TrimRight(strings.TrimSpace(registryURL), "/")
	if registryURL == "" {
		return nil, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, registryURL+"/healthz", nil)
	if err != nil {
		return nil, fmt.Errorf("build relay health request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch relay health: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("relay health status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	var health relayHealthResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&health); err != nil {
		return nil, fmt.Errorf("decode relay health: %w", err)
	}
	return relayBootstrapAddrsFromHealth(health), nil
}

func relayBootstrapAddrsFromHealth(health relayHealthResponse) []string {
	peerID := strings.TrimSpace(health.PeerID)
	values := make([]string, 0, len(health.Addrs))
	for _, addr := range compactNonEmptyStrings(health.Addrs) {
		if !strings.Contains(addr, "/p2p/") && peerID != "" {
			addr += "/p2p/" + peerID
		}
		values = append(values, addr)
	}
	return validRelayBootstrapAddrs(values)
}

func validRelayBootstrapAddrs(values []string) []string {
	values = compactNonEmptyStrings(values)
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		info, err := peer.AddrInfoFromString(value)
		if err != nil || info == nil || info.ID == "" || len(info.Addrs) == 0 {
			continue
		}
		out = append(out, value)
	}
	return compactNonEmptyStrings(out)
}
