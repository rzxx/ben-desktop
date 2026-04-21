package desktopcore

import (
	"ben/registryauth"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	apitypes "ben/desktop/api/types"
	crypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/multiformats/go-multiaddr"
)

const (
	desktopSyncProtocolID       = protocol.ID("/ben/desktop/sync/2.0.0")
	desktopLibraryChangedID     = protocol.ID("/ben/desktop/library-changed/1.0.0")
	desktopCheckpointProtocolID = protocol.ID("/ben/desktop/checkpoint/2.0.0")
	desktopPlaybackProtocolID   = protocol.ID("/ben/desktop/playback/1.0.0")
	desktopArtworkProtocolID    = protocol.ID("/ben/desktop/artwork/1.0.0")
	desktopMembershipProtocolID = protocol.ID("/ben/desktop/membership-refresh/1.0.0")
	transportStreamTimeout      = 20 * time.Second
	transportConnectTimeout     = 10 * time.Second
)

type transportMDNSService interface {
	Start() error
	Close() error
}

type libp2pSyncTransport struct {
	app                    *App
	libraryID              string
	deviceID               string
	host                   host.Host
	mdns                   transportMDNSService
	statusMu               sync.RWMutex
	lastRegistryAnnounceAt *time.Time
	directUpgradeState     string
}

type libp2pSyncPeer struct {
	transport *libp2pSyncTransport
	peerID    peer.ID
	deviceID  string
}

type wireSyncResponse struct {
	SyncResponse
	Error string `json:"error,omitempty"`
}

func (a *App) newLibp2pSyncTransport(ctx context.Context, local apitypes.LocalContext) (managedSyncTransport, error) {
	local.LibraryID = strings.TrimSpace(local.LibraryID)
	local.DeviceID = strings.TrimSpace(local.DeviceID)
	if local.LibraryID == "" {
		return nil, apitypes.ErrNoActiveLibrary
	}
	if local.DeviceID == "" {
		return nil, fmt.Errorf("device id is required")
	}

	hostNode, err := a.newSharedLibp2pHost(libp2pHostBuildOptions{mode: libp2pHostModeClient})
	if err != nil {
		return nil, err
	}

	transport := &libp2pSyncTransport{
		app:       a,
		libraryID: local.LibraryID,
		deviceID:  local.DeviceID,
		host:      hostNode,
	}
	hostNode.SetStreamHandler(desktopSyncProtocolID, transport.handleSyncStream)
	hostNode.SetStreamHandler(desktopLibraryChangedID, transport.handleLibraryChangedStream)
	hostNode.SetStreamHandler(desktopCheckpointProtocolID, transport.handleCheckpointStream)
	hostNode.SetStreamHandler(desktopPlaybackProtocolID, transport.handlePlaybackStream)
	hostNode.SetStreamHandler(desktopArtworkProtocolID, transport.handleArtworkStream)
	hostNode.SetStreamHandler(desktopMembershipProtocolID, transport.handleMembershipRefreshStream)
	hostNode.SetStreamHandler(desktopInviteJoinStartProtocolID, transport.handleInviteJoinStartStream)
	hostNode.SetStreamHandler(desktopInviteJoinStatusProtocolID, transport.handleInviteJoinStatusStream)
	hostNode.SetStreamHandler(desktopInviteJoinCancelProtocolID, transport.handleInviteJoinCancelStream)
	hostNode.Network().Notify(&desktopNetworkNotifee{transport: transport})

	if a.cfg.EnableLANDiscovery {
		service := mdns.NewMdnsService(hostNode, serviceTagForLibrary(local.LibraryID), &desktopMDNSNotifee{
			host:   hostNode,
			logger: a.cfg.Logger,
		})
		if err := service.Start(); err != nil {
			if a.cfg.Logger != nil {
				a.cfg.Logger.Errorf("desktopcore: start mdns failed for %s: %v", local.LibraryID, err)
			}
		} else {
			transport.mdns = service
		}
	}

	if err := a.touchDevicePeerID(ctx, local.DeviceID, hostNode.ID().String(), local.Device); err != nil {
		_ = transport.Close()
		return nil, fmt.Errorf("update local peer id: %w", err)
	}
	a.recordNetworkDebug(apitypes.NetworkDebugTraceEntry{
		Level:     "info",
		Kind:      "transport.started",
		Message:   "libp2p transport started",
		LibraryID: local.LibraryID,
		DeviceID:  local.DeviceID,
		PeerID:    hostNode.ID().String(),
	})
	return transport, nil
}

func (t *libp2pSyncTransport) Close() error {
	if t == nil {
		return nil
	}
	if t.mdns != nil {
		_ = t.mdns.Close()
	}
	if t.host != nil {
		return t.host.Close()
	}
	return nil
}

func (t *libp2pSyncTransport) LocalPeerID() string {
	if t == nil || t.host == nil {
		return ""
	}
	return t.host.ID().String()
}

func (t *libp2pSyncTransport) ListenAddrs() []string {
	if t == nil || t.host == nil {
		return nil
	}
	out := make([]string, 0, len(t.host.Addrs()))
	for _, addr := range t.host.Addrs() {
		out = append(out, fmt.Sprintf("%s/p2p/%s", addr.String(), t.host.ID().String()))
	}
	return sortedListenAddrs(out)
}

func (t *libp2pSyncTransport) ListPeers(ctx context.Context, _ apitypes.LocalContext) ([]SyncPeer, error) {
	if t == nil || t.host == nil {
		return nil, nil
	}
	peerIDs := append([]peer.ID(nil), t.host.Network().Peers()...)
	sort.Slice(peerIDs, func(i, j int) bool {
		return peerIDs[i].String() < peerIDs[j].String()
	})

	out := make([]SyncPeer, 0, len(peerIDs))
	for _, peerID := range peerIDs {
		deviceID, _, err := t.app.memberDeviceIDForPeer(ctx, t.libraryID, peerID.String())
		if err != nil {
			return nil, err
		}
		out = append(out, &libp2pSyncPeer{
			transport: t,
			peerID:    peerID,
			deviceID:  deviceID,
		})
	}
	return out, nil
}

func (t *libp2pSyncTransport) ResolvePeer(ctx context.Context, _ apitypes.LocalContext, peerAddr string) (SyncPeer, error) {
	if t == nil || t.host == nil {
		return nil, fmt.Errorf("transport is not running")
	}
	peerAddr = strings.TrimSpace(peerAddr)
	if peerAddr == "" {
		return nil, fmt.Errorf("peer address is required")
	}

	ma, err := multiaddr.NewMultiaddr(peerAddr)
	if err != nil {
		return nil, fmt.Errorf("parse multiaddr: %w", err)
	}
	info, err := peer.AddrInfoFromP2pAddr(ma)
	if err != nil {
		return nil, fmt.Errorf("peer info from addr: %w", err)
	}
	connectCtx, cancel := context.WithTimeout(ctx, transportConnectTimeout)
	defer cancel()
	t.app.recordNetworkDebug(apitypes.NetworkDebugTraceEntry{
		Level:     "info",
		Kind:      "transport.dial.start",
		Message:   "Dialing peer address",
		LibraryID: t.libraryID,
		DeviceID:  t.deviceID,
		PeerID:    info.ID.String(),
		Address:   peerAddr,
	})
	if err := t.host.Connect(connectCtx, *info); err != nil {
		t.app.recordNetworkDebug(apitypes.NetworkDebugTraceEntry{
			Level:     "error",
			Kind:      "transport.dial.failed",
			Message:   "Dial to peer failed",
			LibraryID: t.libraryID,
			DeviceID:  t.deviceID,
			PeerID:    info.ID.String(),
			Address:   peerAddr,
			Error:     err.Error(),
		})
		return nil, fmt.Errorf("connect peer: %w", err)
	}
	t.app.recordNetworkDebug(apitypes.NetworkDebugTraceEntry{
		Level:     "info",
		Kind:      "transport.dial.succeeded",
		Message:   "Dial to peer succeeded",
		LibraryID: t.libraryID,
		DeviceID:  t.deviceID,
		PeerID:    info.ID.String(),
		Address:   peerAddr,
	})
	_ = t.app.saveKnownPeerAddrs(ctx, t.libraryID, info.ID.String(), []string{peerAddr})

	deviceID, _, err := t.app.memberDeviceIDForPeer(ctx, t.libraryID, info.ID.String())
	if err != nil {
		return nil, err
	}
	return &libp2pSyncPeer{
		transport: t,
		peerID:    info.ID,
		deviceID:  deviceID,
	}, nil
}

func (t *libp2pSyncTransport) ResolvePeerByIdentity(ctx context.Context, _ apitypes.LocalContext, peerID, deviceID string) (SyncPeer, error) {
	if t == nil || t.host == nil {
		return nil, fmt.Errorf("transport is not running")
	}
	peerID = strings.TrimSpace(peerID)
	deviceID = strings.TrimSpace(deviceID)
	if peerID == "" && deviceID != "" {
		var row Device
		if err := t.app.storage.WithContext(ctx).
			Select("peer_id").
			Where("device_id = ?", deviceID).
			Take(&row).Error; err != nil {
			return nil, err
		}
		peerID = strings.TrimSpace(row.PeerID)
	}
	if peerID == "" {
		return nil, fmt.Errorf("peer id is required")
	}
	decoded, err := peer.Decode(peerID)
	if err != nil {
		return nil, fmt.Errorf("decode peer id: %w", err)
	}
	if deviceID == "" {
		resolvedDeviceID, ok, err := t.app.memberDeviceIDForPeer(ctx, t.libraryID, peerID)
		if err != nil {
			return nil, err
		}
		if ok {
			deviceID = resolvedDeviceID
		}
	}
	if t.host.Network().Connectedness(decoded) != network.Connected && t.host.Network().Connectedness(decoded) != network.Limited {
		if err := t.resolvePeerIdentityAddresses(ctx, decoded, deviceID); err != nil && t.app.cfg.Logger != nil {
			t.app.cfg.Logger.Errorf("desktopcore: resolve peer identity addresses failed for %s: %v", decoded.String(), err)
		}
	}
	return &libp2pSyncPeer{
		transport: t,
		peerID:    decoded,
		deviceID:  deviceID,
	}, nil
}

func (t *libp2pSyncTransport) peerForID(ctx context.Context, peerID peer.ID) (SyncPeer, error) {
	if t == nil || t.host == nil || peerID == "" {
		return nil, fmt.Errorf("peer id is required")
	}
	deviceID, _, err := t.app.memberDeviceIDForPeer(ctx, t.libraryID, peerID.String())
	if err != nil {
		return nil, err
	}
	return &libp2pSyncPeer{
		transport: t,
		peerID:    peerID,
		deviceID:  deviceID,
	}, nil
}

func (p *libp2pSyncPeer) Address() string {
	if p == nil || p.transport == nil || p.transport.host == nil {
		return ""
	}
	info := p.transport.host.Peerstore().PeerInfo(p.peerID)
	if len(info.Addrs) > 0 {
		return fmt.Sprintf("%s/p2p/%s", info.Addrs[0].String(), p.peerID.String())
	}
	return p.peerID.String()
}

func (p *libp2pSyncPeer) DeviceID() string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.deviceID)
}

func (p *libp2pSyncPeer) PeerID() string {
	if p == nil {
		return ""
	}
	return p.peerID.String()
}

func (p *libp2pSyncPeer) Sync(ctx context.Context, req SyncRequest) (SyncResponse, error) {
	stream, err := p.openStream(ctx, desktopSyncProtocolID)
	if err != nil {
		return SyncResponse{}, err
	}
	defer stream.Close()

	if err := json.NewEncoder(stream).Encode(req); err != nil {
		return SyncResponse{}, fmt.Errorf("write sync request: %w", err)
	}
	var resp wireSyncResponse
	if err := json.NewDecoder(stream).Decode(&resp); err != nil {
		return SyncResponse{}, fmt.Errorf("read sync response: %w", err)
	}
	if strings.TrimSpace(resp.Error) != "" {
		return SyncResponse{}, fmt.Errorf("%s", strings.TrimSpace(resp.Error))
	}
	return resp.SyncResponse, nil
}

func (p *libp2pSyncPeer) NotifyLibraryChanged(ctx context.Context, req LibraryChangedRequest) (LibraryChangedResponse, error) {
	stream, err := p.openStream(ctx, desktopLibraryChangedID)
	if err != nil {
		return LibraryChangedResponse{}, err
	}
	defer stream.Close()

	if err := json.NewEncoder(stream).Encode(req); err != nil {
		return LibraryChangedResponse{}, fmt.Errorf("write library changed request: %w", err)
	}
	var resp LibraryChangedResponse
	if err := json.NewDecoder(stream).Decode(&resp); err != nil {
		return LibraryChangedResponse{}, fmt.Errorf("read library changed response: %w", err)
	}
	if strings.TrimSpace(resp.Error) != "" {
		return LibraryChangedResponse{}, fmt.Errorf("%s", strings.TrimSpace(resp.Error))
	}
	return resp, nil
}

func (p *libp2pSyncPeer) FetchCheckpoint(ctx context.Context, req CheckpointFetchRequest) (CheckpointFetchResponse, error) {
	stream, err := p.openStream(ctx, desktopCheckpointProtocolID)
	if err != nil {
		return CheckpointFetchResponse{}, err
	}
	defer stream.Close()

	req.LibraryID = strings.TrimSpace(req.LibraryID)
	req.CheckpointID = strings.TrimSpace(req.CheckpointID)
	if err := json.NewEncoder(stream).Encode(req); err != nil {
		return CheckpointFetchResponse{}, fmt.Errorf("write checkpoint request: %w", err)
	}
	var resp CheckpointFetchResponse
	if err := json.NewDecoder(stream).Decode(&resp); err != nil {
		return CheckpointFetchResponse{}, fmt.Errorf("read checkpoint response: %w", err)
	}
	if strings.TrimSpace(resp.Error) != "" {
		return CheckpointFetchResponse{}, fmt.Errorf("%s", strings.TrimSpace(resp.Error))
	}
	return resp, nil
}

func (p *libp2pSyncPeer) FetchPlaybackAsset(ctx context.Context, req PlaybackAssetRequest) (PlaybackAssetResponse, error) {
	stream, err := p.openStream(ctx, desktopPlaybackProtocolID)
	if err != nil {
		return PlaybackAssetResponse{}, err
	}
	defer stream.Close()

	if err := json.NewEncoder(stream).Encode(req); err != nil {
		return PlaybackAssetResponse{}, fmt.Errorf("write playback request: %w", err)
	}
	var resp PlaybackAssetResponse
	if err := json.NewDecoder(stream).Decode(&resp); err != nil {
		return PlaybackAssetResponse{}, fmt.Errorf("read playback response: %w", err)
	}
	if strings.TrimSpace(resp.Error) != "" {
		return PlaybackAssetResponse{}, fmt.Errorf("%s", strings.TrimSpace(resp.Error))
	}
	return resp, nil
}

func (p *libp2pSyncPeer) FetchArtworkBlob(ctx context.Context, req ArtworkBlobRequest) (ArtworkBlobResponse, error) {
	stream, err := p.openStream(ctx, desktopArtworkProtocolID)
	if err != nil {
		return ArtworkBlobResponse{}, err
	}
	defer stream.Close()

	if err := json.NewEncoder(stream).Encode(req); err != nil {
		return ArtworkBlobResponse{}, fmt.Errorf("write artwork request: %w", err)
	}
	var resp ArtworkBlobResponse
	if err := json.NewDecoder(stream).Decode(&resp); err != nil {
		return ArtworkBlobResponse{}, fmt.Errorf("read artwork response: %w", err)
	}
	if strings.TrimSpace(resp.Error) != "" {
		return ArtworkBlobResponse{}, fmt.Errorf("%s", strings.TrimSpace(resp.Error))
	}
	return resp, nil
}

func (p *libp2pSyncPeer) RefreshMembership(ctx context.Context, req MembershipRefreshRequest) (MembershipRefreshResponse, error) {
	stream, err := p.openStream(ctx, desktopMembershipProtocolID)
	if err != nil {
		return MembershipRefreshResponse{}, err
	}
	defer stream.Close()

	if err := json.NewEncoder(stream).Encode(req); err != nil {
		return MembershipRefreshResponse{}, fmt.Errorf("write membership refresh request: %w", err)
	}
	var resp MembershipRefreshResponse
	if err := json.NewDecoder(stream).Decode(&resp); err != nil {
		return MembershipRefreshResponse{}, fmt.Errorf("read membership refresh response: %w", err)
	}
	if strings.TrimSpace(resp.Error) != "" {
		return MembershipRefreshResponse{}, fmt.Errorf("%s", strings.TrimSpace(resp.Error))
	}
	return resp, nil
}

func (p *libp2pSyncPeer) openStream(ctx context.Context, protocolID protocol.ID) (network.Stream, error) {
	if p == nil || p.transport == nil || p.transport.host == nil {
		return nil, fmt.Errorf("peer transport is not available")
	}
	if p.transport.app.cfg.RequireDirectForLargeTransfers && protocolRequiresDirectConnection(protocolID) {
		p.transport.app.recordNetworkDebug(apitypes.NetworkDebugTraceEntry{
			Level:              "info",
			Kind:               "transport.direct_upgrade.started",
			Message:            "Attempting direct connection upgrade",
			LibraryID:          p.transport.libraryID,
			DeviceID:           p.transport.deviceID,
			PeerID:             p.peerID.String(),
			Address:            p.Address(),
			DirectUpgradeState: "dcutr_upgrade_in_progress",
		})
		p.transport.setDirectUpgradeState("dcutr_upgrade_in_progress")
		state, err := ensureDirectConnection(ctx, p.transport.host, p.peerID)
		p.transport.recordConnectionState(peerConnectionStateDebugEntry{
			level:   "info",
			peerID:  p.peerID.String(),
			address: p.Address(),
			state:   state,
		})
		if err != nil {
			p.transport.app.recordNetworkDebug(apitypes.NetworkDebugTraceEntry{
				Level:              "warn",
				Kind:               "transport.direct_upgrade.failed",
				Message:            "Direct connection required but only relayed connectivity is available",
				LibraryID:          p.transport.libraryID,
				DeviceID:           p.transport.deviceID,
				PeerID:             p.peerID.String(),
				Address:            p.Address(),
				ConnectionKind:     state.Kind(),
				DirectUpgradeState: "direct_upgrade_failed",
				Error:              err.Error(),
			})
			if state.OnlyLimitedRelayed {
				p.transport.app.recordNetworkDebug(apitypes.NetworkDebugTraceEntry{
					Level:              "warn",
					Kind:               "transport.relayed_only",
					Message:            "Peer is reachable only over a limited relayed connection",
					LibraryID:          p.transport.libraryID,
					DeviceID:           p.transport.deviceID,
					PeerID:             p.peerID.String(),
					Address:            p.Address(),
					ConnectionKind:     "relayed",
					DirectUpgradeState: "relayed_connection_only",
					Error:              errDirectConnectionRequired.Error(),
				})
			}
			p.transport.setDirectUpgradeState("direct_upgrade_failed")
			return nil, err
		}
		p.transport.app.recordNetworkDebug(apitypes.NetworkDebugTraceEntry{
			Level:              "info",
			Kind:               "transport.direct_upgrade.succeeded",
			Message:            "Direct connection ready",
			LibraryID:          p.transport.libraryID,
			DeviceID:           p.transport.deviceID,
			PeerID:             p.peerID.String(),
			Address:            p.Address(),
			ConnectionKind:     state.Kind(),
			DirectUpgradeState: "direct_upgrade_succeeded",
		})
		p.transport.setDirectUpgradeState("direct_upgrade_succeeded")
	}
	if protocolAllowsLimitedConnection(protocolID) {
		ctx = network.WithAllowLimitedConn(ctx, string(protocolID))
	}
	stream, err := p.transport.host.NewStream(ctx, p.peerID, protocolID)
	if err != nil {
		return nil, fmt.Errorf("open stream: %w", err)
	}
	_ = stream.SetDeadline(time.Now().Add(transportStreamTimeout))
	return stream, nil
}

func (t *libp2pSyncTransport) handleSyncStream(stream network.Stream) {
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(transportStreamTimeout))

	ctx, cancel := context.WithTimeout(context.Background(), transportStreamTimeout)
	defer cancel()

	var req SyncRequest
	if err := json.NewDecoder(stream).Decode(&req); err != nil {
		t.writeSyncError(stream, fmt.Sprintf("decode request: %v", err))
		return
	}
	if strings.TrimSpace(req.LibraryID) != strings.TrimSpace(t.libraryID) {
		t.writeSyncError(stream, "library mismatch")
		return
	}
	if _, err := t.app.verifyTransportPeerAuth(ctx, t.libraryID, req.DeviceID, req.PeerID, stream.Conn().RemotePeer().String(), req.Auth); err != nil {
		t.writeSyncError(stream, err.Error())
		return
	}

	resp, err := t.app.buildSyncResponse(ctx, req)
	if err != nil {
		t.writeSyncError(stream, err.Error())
		return
	}
	if err := json.NewEncoder(stream).Encode(wireSyncResponse{SyncResponse: resp}); err != nil {
		t.app.logf("desktopcore: write sync response failed: %v", err)
	}
}

func (t *libp2pSyncTransport) handleLibraryChangedStream(stream network.Stream) {
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(transportStreamTimeout))

	ctx, cancel := context.WithTimeout(context.Background(), transportStreamTimeout)
	defer cancel()

	var req LibraryChangedRequest
	if err := json.NewDecoder(stream).Decode(&req); err != nil {
		t.writeLibraryChangedError(stream, fmt.Sprintf("decode request: %v", err))
		return
	}
	runtime := t.app.transportService.activeRuntimeForLibrary(t.libraryID)
	if runtime == nil || runtime.transport != t {
		t.writeLibraryChangedError(stream, "transport runtime is not active")
		return
	}
	resp, err := t.app.transportService.handleLibraryChangedSignal(ctx, runtime, stream.Conn().RemotePeer().String(), req)
	if err != nil {
		t.writeLibraryChangedError(stream, err.Error())
		return
	}
	if err := json.NewEncoder(stream).Encode(resp); err != nil {
		t.app.logf("desktopcore: write library changed response failed: %v", err)
	}
}

func (t *libp2pSyncTransport) handleCheckpointStream(stream network.Stream) {
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(transportStreamTimeout))

	ctx, cancel := context.WithTimeout(context.Background(), transportStreamTimeout)
	defer cancel()

	var req CheckpointFetchRequest
	if err := json.NewDecoder(stream).Decode(&req); err != nil {
		t.writeCheckpointError(stream, fmt.Sprintf("decode request: %v", err))
		return
	}
	if strings.TrimSpace(req.LibraryID) != strings.TrimSpace(t.libraryID) {
		t.writeCheckpointError(stream, "library mismatch")
		return
	}
	if _, err := t.app.verifyTransportPeerAuth(ctx, t.libraryID, req.Auth.Cert.DeviceID, req.Auth.Cert.PeerID, stream.Conn().RemotePeer().String(), req.Auth); err != nil {
		t.writeCheckpointError(stream, err.Error())
		return
	}

	resp, err := t.app.buildCheckpointFetchResponse(ctx, req)
	if err != nil {
		t.writeCheckpointError(stream, err.Error())
		return
	}
	if err := json.NewEncoder(stream).Encode(resp); err != nil {
		t.app.logf("desktopcore: write checkpoint response failed: %v", err)
	}
}

func (t *libp2pSyncTransport) handlePlaybackStream(stream network.Stream) {
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(transportStreamTimeout))

	ctx, cancel := context.WithTimeout(context.Background(), transportStreamTimeout)
	defer cancel()

	var req PlaybackAssetRequest
	if err := json.NewDecoder(stream).Decode(&req); err != nil {
		t.writePlaybackError(stream, fmt.Sprintf("decode request: %v", err))
		return
	}
	if strings.TrimSpace(req.LibraryID) != strings.TrimSpace(t.libraryID) {
		t.writePlaybackError(stream, "library mismatch")
		return
	}
	if _, err := t.app.verifyTransportPeerAuth(ctx, t.libraryID, req.DeviceID, req.PeerID, stream.Conn().RemotePeer().String(), req.Auth); err != nil {
		t.writePlaybackError(stream, err.Error())
		return
	}

	resp, err := t.app.buildPlaybackAssetResponse(ctx, req)
	if err != nil {
		t.writePlaybackError(stream, err.Error())
		return
	}
	if err := json.NewEncoder(stream).Encode(resp); err != nil {
		t.app.logf("desktopcore: write playback response failed: %v", err)
	}
}

func (t *libp2pSyncTransport) handleArtworkStream(stream network.Stream) {
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(transportStreamTimeout))

	ctx, cancel := context.WithTimeout(context.Background(), transportStreamTimeout)
	defer cancel()

	var req ArtworkBlobRequest
	if err := json.NewDecoder(stream).Decode(&req); err != nil {
		t.writeArtworkError(stream, fmt.Sprintf("decode request: %v", err))
		return
	}
	if strings.TrimSpace(req.LibraryID) != strings.TrimSpace(t.libraryID) {
		t.writeArtworkError(stream, "library mismatch")
		return
	}
	if _, err := t.app.verifyTransportPeerAuth(ctx, t.libraryID, req.DeviceID, req.PeerID, stream.Conn().RemotePeer().String(), req.Auth); err != nil {
		t.writeArtworkError(stream, err.Error())
		return
	}

	resp, err := t.app.buildArtworkBlobResponse(ctx, req)
	if err != nil {
		t.writeArtworkError(stream, err.Error())
		return
	}
	if err := json.NewEncoder(stream).Encode(resp); err != nil {
		t.app.logf("desktopcore: write artwork response failed: %v", err)
	}
}

func (t *libp2pSyncTransport) handleMembershipRefreshStream(stream network.Stream) {
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(transportStreamTimeout))

	ctx, cancel := context.WithTimeout(context.Background(), transportStreamTimeout)
	defer cancel()

	var req MembershipRefreshRequest
	if err := json.NewDecoder(stream).Decode(&req); err != nil {
		t.writeMembershipRefreshError(stream, fmt.Sprintf("decode request: %v", err))
		return
	}
	if strings.TrimSpace(req.LibraryID) != strings.TrimSpace(t.libraryID) {
		t.writeMembershipRefreshError(stream, "library mismatch")
		return
	}
	if strings.TrimSpace(req.PeerID) == "" {
		req.PeerID = stream.Conn().RemotePeer().String()
	}

	resp, err := t.app.buildMembershipRefreshResponse(ctx, req)
	if err != nil {
		t.writeMembershipRefreshError(stream, err.Error())
		return
	}
	if err := json.NewEncoder(stream).Encode(resp); err != nil {
		t.app.logf("desktopcore: write membership refresh response failed: %v", err)
	}
}

func (t *libp2pSyncTransport) handleInviteJoinStartStream(stream network.Stream) {
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(transportStreamTimeout))

	ctx, cancel := context.WithTimeout(context.Background(), transportStreamTimeout)
	defer cancel()

	var req inviteJoinStartRequest
	if err := json.NewDecoder(stream).Decode(&req); err != nil {
		_ = json.NewEncoder(stream).Encode(inviteJoinStartResponse{Error: fmt.Sprintf("decode request: %v", err)})
		return
	}

	resp, err := t.app.handleInviteJoinStart(ctx, t.libraryID, stream.Conn().LocalPeer().String(), stream.Conn().RemotePeer().String(), req)
	if err != nil {
		_ = json.NewEncoder(stream).Encode(inviteJoinStartResponse{Error: strings.TrimSpace(err.Error())})
		return
	}
	if err := json.NewEncoder(stream).Encode(resp); err != nil {
		t.app.logf("desktopcore: write invite start response failed: %v", err)
	}
}

func (t *libp2pSyncTransport) handleInviteJoinStatusStream(stream network.Stream) {
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(transportStreamTimeout))

	ctx, cancel := context.WithTimeout(context.Background(), transportStreamTimeout)
	defer cancel()

	var req inviteJoinStatusRequest
	if err := json.NewDecoder(stream).Decode(&req); err != nil {
		_ = json.NewEncoder(stream).Encode(inviteJoinStatusResponse{Error: fmt.Sprintf("decode request: %v", err)})
		return
	}

	resp, err := t.app.handleInviteJoinStatus(ctx, t.libraryID, stream.Conn().LocalPeer().String(), stream.Conn().RemotePeer().String(), req)
	if err != nil {
		_ = json.NewEncoder(stream).Encode(inviteJoinStatusResponse{Error: strings.TrimSpace(err.Error())})
		return
	}
	if err := json.NewEncoder(stream).Encode(resp); err != nil {
		t.app.logf("desktopcore: write invite status response failed: %v", err)
	}
}

func (t *libp2pSyncTransport) handleInviteJoinCancelStream(stream network.Stream) {
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(transportStreamTimeout))

	ctx, cancel := context.WithTimeout(context.Background(), transportStreamTimeout)
	defer cancel()

	var req inviteJoinCancelRequest
	if err := json.NewDecoder(stream).Decode(&req); err != nil {
		_ = json.NewEncoder(stream).Encode(inviteJoinCancelResponse{Error: fmt.Sprintf("decode request: %v", err)})
		return
	}

	resp, err := t.app.handleInviteJoinCancel(ctx, t.libraryID, stream.Conn().RemotePeer().String(), req)
	if err != nil {
		_ = json.NewEncoder(stream).Encode(inviteJoinCancelResponse{Error: strings.TrimSpace(err.Error())})
		return
	}
	if err := json.NewEncoder(stream).Encode(resp); err != nil {
		t.app.logf("desktopcore: write invite cancel response failed: %v", err)
	}
}

func (t *libp2pSyncTransport) writeSyncError(stream network.Stream, message string) {
	_ = json.NewEncoder(stream).Encode(wireSyncResponse{Error: strings.TrimSpace(message)})
}

func (t *libp2pSyncTransport) writeLibraryChangedError(stream network.Stream, message string) {
	_ = json.NewEncoder(stream).Encode(LibraryChangedResponse{Error: strings.TrimSpace(message)})
}

func (t *libp2pSyncTransport) writeCheckpointError(stream network.Stream, message string) {
	_ = json.NewEncoder(stream).Encode(CheckpointFetchResponse{Error: strings.TrimSpace(message)})
}

func (t *libp2pSyncTransport) writePlaybackError(stream network.Stream, message string) {
	_ = json.NewEncoder(stream).Encode(PlaybackAssetResponse{Error: strings.TrimSpace(message)})
}

func (t *libp2pSyncTransport) writeArtworkError(stream network.Stream, message string) {
	_ = json.NewEncoder(stream).Encode(ArtworkBlobResponse{Error: strings.TrimSpace(message)})
}

func (t *libp2pSyncTransport) writeMembershipRefreshError(stream network.Stream, message string) {
	_ = json.NewEncoder(stream).Encode(MembershipRefreshResponse{Error: strings.TrimSpace(message)})
}

type desktopMDNSNotifee struct {
	host   host.Host
	logger apitypes.Logger
}

func (n *desktopMDNSNotifee) HandlePeerFound(info peer.AddrInfo) {
	if n == nil || n.host == nil || info.ID == n.host.ID() {
		return
	}
	if !shouldInitiateTransportDial(n.host.ID(), info.ID) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), transportConnectTimeout)
	defer cancel()
	if err := n.host.Connect(ctx, info); err != nil {
		if n.logger != nil {
			n.logger.Errorf("desktopcore: mdns connect to %s failed: %v", info.ID.String(), err)
		}
		return
	}
	if n.logger != nil {
		n.logger.Printf("desktopcore: discovered peer %s", info.ID.String())
	}
}

func shouldInitiateTransportDial(local, remote peer.ID) bool {
	if local == "" || remote == "" {
		return true
	}
	return local.String() < remote.String()
}

type desktopNetworkNotifee struct {
	transport *libp2pSyncTransport
}

func (n *desktopNetworkNotifee) Listen(network.Network, multiaddr.Multiaddr)      {}
func (n *desktopNetworkNotifee) ListenClose(network.Network, multiaddr.Multiaddr) {}
func (n *desktopNetworkNotifee) OpenedStream(network.Network, network.Stream)     {}
func (n *desktopNetworkNotifee) ClosedStream(network.Network, network.Stream)     {}

func (n *desktopNetworkNotifee) Connected(_ network.Network, conn network.Conn) {
	if n == nil || n.transport == nil || conn == nil {
		return
	}
	peerID := conn.RemotePeer()
	if peerID == "" {
		return
	}
	go n.transport.handlePeerConnected(peerID)
}

func (n *desktopNetworkNotifee) Disconnected(net network.Network, conn network.Conn) {
	if n == nil || n.transport == nil || conn == nil {
		return
	}
	peerID := conn.RemotePeer()
	if peerID == "" || net == nil {
		return
	}
	if net.Connectedness(peerID) == network.Connected {
		return
	}
	go n.transport.handlePeerDisconnected(peerID)
}

func (t *libp2pSyncTransport) handlePeerConnected(peerID peer.ID) {
	if t == nil || t.app == nil || t.host == nil || peerID == "" || peerID == t.host.ID() {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), transportStreamTimeout)
	defer cancel()

	if deviceID, ok, err := t.app.memberDeviceIDForPeer(ctx, t.libraryID, peerID.String()); err != nil {
		t.app.logf("desktopcore: resolve connected peer %s failed: %v", peerID.String(), err)
	} else if ok {
		state := connectionStateForPeer(t.host, peerID)
		t.app.recordNetworkDebug(apitypes.NetworkDebugTraceEntry{
			Level:          "info",
			Kind:           "peer.connected",
			Message:        "Peer connection established",
			LibraryID:      t.libraryID,
			DeviceID:       deviceID,
			PeerID:         peerID.String(),
			ConnectionKind: state.Kind(),
		})
		if err := t.app.updateDevicePeerID(ctx, t.libraryID, deviceID, peerID.String(), deviceID); err != nil {
			t.app.logf("desktopcore: touch connected peer %s failed: %v", peerID.String(), err)
		}
		if info := t.host.Peerstore().PeerInfo(peerID); len(info.Addrs) > 0 {
			addrs := make([]string, 0, len(info.Addrs))
			for _, addr := range info.Addrs {
				addrs = append(addrs, fmt.Sprintf("%s/p2p/%s", addr.String(), peerID.String()))
			}
			_ = t.app.saveKnownPeerAddrs(ctx, t.libraryID, peerID.String(), addrs)
		}
	} else {
		t.app.recordNetworkDebug(apitypes.NetworkDebugTraceEntry{
			Level:     "info",
			Kind:      "peer.connected",
			Message:   "Peer connection established for unresolved membership",
			LibraryID: t.libraryID,
			PeerID:    peerID.String(),
		})
		return
	}

	local, err := t.app.EnsureLocalContext(ctx)
	if err != nil {
		return
	}
	if strings.TrimSpace(local.LibraryID) != strings.TrimSpace(t.libraryID) || strings.TrimSpace(local.DeviceID) != strings.TrimSpace(t.deviceID) {
		return
	}
	runtime := t.app.transportService.activeRuntimeForLibrary(t.libraryID)
	if runtime == nil || runtime.transport != t || strings.TrimSpace(t.LocalPeerID()) == "" {
		return
	}
	syncPeer, err := t.peerForID(ctx, peerID)
	if err != nil {
		t.app.logf("desktopcore: prepare connected peer catch-up for %s failed: %v", peerID.String(), err)
		return
	}
	t.app.transportService.scheduleRuntimeCatchupPeer(runtime, apitypes.NetworkSyncReasonConnect, syncPeer, 0)
}

func (t *libp2pSyncTransport) handlePeerDisconnected(peerID peer.ID) {
	if t == nil || t.app == nil || t.host == nil || peerID == "" || peerID == t.host.ID() {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), transportConnectTimeout)
	defer cancel()
	t.app.recordNetworkDebug(apitypes.NetworkDebugTraceEntry{
		Level:          "warn",
		Kind:           "peer.disconnected",
		Message:        "Peer disconnected",
		LibraryID:      t.libraryID,
		PeerID:         peerID.String(),
		ConnectionKind: "disconnected",
	})

	if err := t.app.markDevicePresenceOffline(ctx, t.libraryID, peerID.String()); err != nil {
		t.app.logf("desktopcore: mark disconnected peer offline failed for %s: %v", peerID.String(), err)
	}
}

func protocolAllowsLimitedConnection(protocolID protocol.ID) bool {
	switch protocolID {
	case desktopSyncProtocolID,
		desktopLibraryChangedID,
		desktopMembershipProtocolID,
		desktopInviteJoinStartProtocolID,
		desktopInviteJoinStatusProtocolID,
		desktopInviteJoinCancelProtocolID:
		return true
	default:
		return false
	}
}

func protocolRequiresDirectConnection(protocolID protocol.ID) bool {
	switch protocolID {
	case desktopCheckpointProtocolID, desktopPlaybackProtocolID, desktopArtworkProtocolID:
		return true
	default:
		return false
	}
}

type peerConnectionStateDebugEntry struct {
	level   string
	peerID  string
	address string
	state   peerConnectionState
}

func (t *libp2pSyncTransport) recordConnectionState(entry peerConnectionStateDebugEntry) {
	if t == nil || t.app == nil {
		return
	}
	level := strings.TrimSpace(entry.level)
	if level == "" {
		level = "info"
	}
	t.app.recordNetworkDebug(apitypes.NetworkDebugTraceEntry{
		Level:          level,
		Kind:           "transport.connection.state",
		Message:        "Connection state updated",
		LibraryID:      t.libraryID,
		DeviceID:       t.deviceID,
		PeerID:         strings.TrimSpace(entry.peerID),
		Address:        strings.TrimSpace(entry.address),
		ConnectionKind: entry.state.Kind(),
	})
}

func (t *libp2pSyncTransport) resolvePeerIdentityAddresses(ctx context.Context, peerID peer.ID, deviceID string) error {
	if t == nil || t.host == nil || peerID == "" {
		return nil
	}
	addrs := make([]string, 0, 8)
	if locator := t.app.peerLocator(t.app.cfg.RegistryURL); locator != nil {
		auth, authErr := t.app.registryMembershipAuth(ctx, t.libraryID, t.deviceID, t.host.ID().String())
		record, ok, err := PeerPresenceRecord{}, false, authErr
		if authErr == nil {
			record, ok, err = locator.LookupMemberPeer(ctx, registryauth.MemberLookupRequest{
				LibraryID:     t.libraryID,
				PeerID:        peerID.String(),
				RootPublicKey: auth.RootPublicKey,
				Auth:          auth.Auth,
			})
		}
		if err == nil && ok {
			addrs = append(addrs, record.Addrs...)
		} else if err != nil {
			t.app.recordNetworkDebug(apitypes.NetworkDebugTraceEntry{
				Level:     "warn",
				Kind:      "registry.lookup.failed",
				Message:   "Registry member lookup failed",
				LibraryID: t.libraryID,
				DeviceID:  t.deviceID,
				PeerID:    peerID.String(),
				Error:     err.Error(),
			})
		}
	}
	cached, err := t.app.loadKnownPeerAddrs(ctx, t.libraryID, peerID.String())
	if err == nil {
		addrs = append(addrs, cached...)
	}
	for _, addr := range compactNonEmptyStrings(addrs) {
		if _, err := t.ResolvePeer(ctx, apitypes.LocalContext{}, addr); err == nil {
			return nil
		}
	}
	if deviceID != "" {
		t.app.recordNetworkDebug(apitypes.NetworkDebugTraceEntry{
			Level:     "warn",
			Kind:      "transport.peer.lookup.missed",
			Message:   "No peer addresses resolved for identity",
			LibraryID: t.libraryID,
			DeviceID:  deviceID,
			PeerID:    peerID.String(),
		})
	}
	return nil
}

func (t *libp2pSyncTransport) setLastRegistryAnnounceAt(value time.Time) {
	if t == nil {
		return
	}
	t.statusMu.Lock()
	defer t.statusMu.Unlock()
	copyValue := value.UTC()
	t.lastRegistryAnnounceAt = &copyValue
}

func (t *libp2pSyncTransport) setDirectUpgradeState(value string) {
	if t == nil {
		return
	}
	t.statusMu.Lock()
	defer t.statusMu.Unlock()
	t.directUpgradeState = strings.TrimSpace(value)
}

func (t *libp2pSyncTransport) appendNetworkStatus(out *apitypes.NetworkStatus) {
	if t == nil || out == nil {
		return
	}
	t.statusMu.RLock()
	lastRegistryAnnounceAt := cloneTimePtr(t.lastRegistryAnnounceAt)
	directUpgradeState := strings.TrimSpace(t.directUpgradeState)
	t.statusMu.RUnlock()

	out.RelayReservationActive = len(advertisedRelayAddrs(t.host)) > 0
	out.AdvertisedRelayAddrs = advertisedRelayAddrs(t.host)
	out.LastRegistryAnnounceAt = lastRegistryAnnounceAt
	out.DirectUpgradeState = directUpgradeState

	directPeers := 0
	limitedPeers := 0
	for _, peerID := range t.host.Network().Peers() {
		state := connectionStateForPeer(t.host, peerID)
		switch {
		case state.Direct:
			directPeers++
		case state.OnlyLimitedRelayed:
			limitedPeers++
		}
	}
	switch {
	case directPeers > 0 && limitedPeers > 0:
		out.ConnectionKind = "mixed"
	case directPeers > 0:
		out.ConnectionKind = "direct"
	case limitedPeers > 0:
		out.ConnectionKind = "relayed"
	default:
		out.ConnectionKind = "disconnected"
	}
}

func loadOrCreateTransportIdentityKey(path string) (crypto.PrivKey, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("identity key path is required")
	}
	if data, err := os.ReadFile(path); err == nil {
		key, err := crypto.UnmarshalPrivateKey(data)
		if err != nil {
			return nil, fmt.Errorf("decode private key: %w", err)
		}
		return key, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read private key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create identity directory: %w", err)
	}
	priv, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate private key: %w", err)
	}
	encoded, err := crypto.MarshalPrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("encode private key: %w", err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return nil, fmt.Errorf("write private key: %w", err)
	}
	return priv, nil
}
