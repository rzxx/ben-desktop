package desktopcore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
	relayv2 "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
)

var errDirectConnectionRequired = errors.New("direct_connection_required")

type libp2pHostMode int

const (
	libp2pHostModeClient libp2pHostMode = iota
	libp2pHostModeRelayServer
)

type libp2pHostBuildOptions struct {
	mode                libp2pHostMode
	relayBootstrapAddrs []string
}

type peerConnectionState struct {
	Any                  bool
	Direct               bool
	OnlyLimitedRelayed   bool
	HasLimitedConnection bool
}

func (s peerConnectionState) Kind() string {
	switch {
	case s.Direct && s.HasLimitedConnection:
		return "mixed"
	case s.Direct:
		return "direct"
	case s.OnlyLimitedRelayed:
		return "relayed"
	default:
		return "disconnected"
	}
}

func (a *App) newSharedLibp2pHost(opts libp2pHostBuildOptions) (host.Host, error) {
	priv, err := loadOrCreateTransportIdentityKey(a.cfg.IdentityKeyPath)
	if err != nil {
		return nil, fmt.Errorf("load transport identity: %w", err)
	}

	staticRelays, err := parseRelayBootstrapAddrInfos(a.relayBootstrapAddrsForHost(opts.relayBootstrapAddrs))
	if err != nil {
		return nil, err
	}

	libp2pOpts := []libp2p.Option{
		libp2p.Identity(priv),
		libp2p.ListenAddrStrings(
			"/ip4/0.0.0.0/tcp/0",
			"/ip6/::/tcp/0",
			"/ip4/0.0.0.0/udp/0/quic-v1",
			"/ip6/::/udp/0/quic-v1",
		),
		libp2p.NATPortMap(),
		libp2p.EnableNATService(),
		libp2p.EnableRelay(),
		libp2p.EnableHolePunching(),
		libp2p.EnableAutoNATv2(),
	}

	if len(staticRelays) > 0 {
		libp2pOpts = append(
			libp2pOpts,
			libp2p.EnableAutoRelayWithStaticRelays(staticRelays),
		)
	}

	if opts.mode == libp2pHostModeRelayServer {
		rm, err := newRelayResourceManager()
		if err != nil {
			return nil, fmt.Errorf("build relay resource manager: %w", err)
		}
		libp2pOpts = append(libp2pOpts,
			libp2p.ResourceManager(rm),
			libp2p.ForceReachabilityPublic(),
			libp2p.EnableRelayService(relayv2.WithResources(newRelayResources())),
		)
	}

	hostNode, err := libp2p.New(libp2pOpts...)
	if err != nil {
		return nil, fmt.Errorf("create libp2p host: %w", err)
	}
	if opts.mode == libp2pHostModeClient && len(staticRelays) > 0 {
		a.bootstrapStaticRelayConnections(hostNode, staticRelays)
	}
	return hostNode, nil
}

func (a *App) relayBootstrapAddrsForHost(extra []string) []string {
	if a == nil {
		return compactNonEmptyStrings(extra)
	}
	return compactNonEmptyStrings(append(append([]string(nil), extra...), a.cfg.RelayBootstrapAddrs...))
}

func parseRelayBootstrapAddrInfos(values []string) ([]peer.AddrInfo, error) {
	values = compactNonEmptyStrings(values)
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]peer.AddrInfo, 0, len(values))
	for _, value := range values {
		info, err := peer.AddrInfoFromString(value)
		if err != nil {
			return nil, fmt.Errorf("parse relay bootstrap addr %q: %w", value, err)
		}
		out = append(out, *info)
	}
	return out, nil
}

func (a *App) bootstrapStaticRelayConnections(h host.Host, relays []peer.AddrInfo) {
	if h == nil || len(relays) == 0 {
		return
	}
	go func() {
		for _, info := range relays {
			connectCtx, cancel := context.WithTimeout(context.Background(), transportConnectTimeout)
			err := h.Connect(connectCtx, info)
			cancel()
			if err != nil && a != nil && a.cfg.Logger != nil {
				a.cfg.Logger.Printf("desktopcore: relay bootstrap connect to %s failed: %v", info.ID.String(), err)
			}
		}
	}()
}

func (a *App) ensureActiveTransportRelayReservation(ctx context.Context, timeout time.Duration) error {
	if a == nil {
		return nil
	}
	transport, ok := a.activeSyncTransport().(*libp2pSyncTransport)
	if !ok || transport == nil || transport.host == nil {
		return nil
	}
	if len(advertisedRelayAddrs(transport.host)) > 0 {
		return nil
	}
	relays, err := parseRelayBootstrapAddrInfos(a.relayBootstrapAddrsForHost(nil))
	if err != nil {
		return err
	}
	if len(relays) == 0 {
		return nil
	}
	if timeout <= 0 {
		timeout = transportConnectTimeout
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var lastErr error
	for {
		if len(advertisedRelayAddrs(transport.host)) > 0 {
			return nil
		}
		for _, info := range relays {
			if waitCtx.Err() != nil {
				break
			}
			connectTimeout := transportConnectTimeout
			if deadline, ok := waitCtx.Deadline(); ok {
				if remaining := time.Until(deadline); remaining > 0 && remaining < connectTimeout {
					connectTimeout = remaining
				}
			}
			if connectTimeout <= 0 {
				break
			}
			connectCtx, connectCancel := context.WithTimeout(waitCtx, connectTimeout)
			err := transport.host.Connect(connectCtx, info)
			connectCancel()
			if err != nil {
				lastErr = err
			}
			if len(advertisedRelayAddrs(transport.host)) > 0 {
				return nil
			}
		}
		if waitCtx.Err() != nil {
			break
		}
		select {
		case <-waitCtx.Done():
		case <-time.After(100 * time.Millisecond):
		}
	}
	if len(advertisedRelayAddrs(transport.host)) > 0 {
		return nil
	}
	if lastErr != nil {
		return fmt.Errorf("ensure active transport relay reservation: %w", lastErr)
	}
	return context.DeadlineExceeded
}

func newRelayResources() relayv2.Resources {
	resources := relayv2.DefaultResources()
	resources.ReservationTTL = time.Hour
	resources.MaxReservations = 128
	resources.MaxCircuits = 8
	resources.MaxReservationsPerPeer = 1
	resources.MaxReservationsPerIP = 8
	resources.MaxReservationsPerASN = 32
	resources.Limit = &relayv2.RelayLimit{
		Duration: 90 * time.Second,
		Data:     256 << 10,
	}
	return resources
}

func newRelayResourceManager() (network.ResourceManager, error) {
	scaling := rcmgr.DefaultLimits
	libp2p.SetDefaultServiceLimits(&scaling)
	limits := scaling.AutoScale()
	return rcmgr.NewResourceManager(rcmgr.NewFixedLimiter(limits), rcmgr.WithMetricsDisabled())
}

func connectionStateForPeer(h host.Host, peerID peer.ID) peerConnectionState {
	if h == nil || peerID == "" {
		return peerConnectionState{}
	}
	state := peerConnectionState{}
	for _, conn := range h.Network().ConnsToPeer(peerID) {
		if conn == nil {
			continue
		}
		state.Any = true
		if conn.Stat().Limited {
			state.HasLimitedConnection = true
			continue
		}
		state.Direct = true
	}
	state.OnlyLimitedRelayed = state.Any && !state.Direct && state.HasLimitedConnection
	return state
}

func ensureDirectConnection(ctx context.Context, h host.Host, peerID peer.ID) (peerConnectionState, error) {
	state := connectionStateForPeer(h, peerID)
	if state.Direct {
		return state, nil
	}
	if h == nil || peerID == "" {
		return state, errDirectConnectionRequired
	}
	dialCtx := network.WithForceDirectDial(ctx, "direct-required")
	info := peer.AddrInfo{
		ID:    peerID,
		Addrs: h.Peerstore().Addrs(peerID),
	}
	if err := h.Connect(dialCtx, info); err != nil {
		next := connectionStateForPeer(h, peerID)
		if next.Direct {
			return next, nil
		}
		if next.OnlyLimitedRelayed {
			return next, fmt.Errorf("%w: %v", errDirectConnectionRequired, err)
		}
		return next, err
	}
	state = connectionStateForPeer(h, peerID)
	if state.Direct {
		return state, nil
	}
	return state, errDirectConnectionRequired
}

func advertisedRelayAddrs(h host.Host) []string {
	if h == nil {
		return nil
	}
	out := make([]string, 0, len(h.Addrs()))
	for _, addr := range h.Addrs() {
		value := addr.String()
		if !strings.Contains(value, "/p2p-circuit") {
			continue
		}
		out = append(out, fmt.Sprintf("%s/p2p/%s", value, h.ID().String()))
	}
	return sortedListenAddrs(out)
}
