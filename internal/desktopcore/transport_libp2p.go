package desktopcore

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	apitypes "ben/desktop/api/types"
	"github.com/libp2p/go-libp2p"
	crypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	tcp "github.com/libp2p/go-libp2p/p2p/transport/tcp"
	"github.com/multiformats/go-multiaddr"
)

const (
	desktopSyncProtocolID       = protocol.ID("/ben/desktop/sync/2.0.0")
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
	app       *App
	libraryID string
	deviceID  string
	host      host.Host
	mdns      transportMDNSService
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

	priv, err := loadOrCreateTransportIdentityKey(a.cfg.IdentityKeyPath)
	if err != nil {
		return nil, fmt.Errorf("load transport identity: %w", err)
	}
	hostNode, err := libp2p.New(
		libp2p.Identity(priv),
		libp2p.ListenAddrStrings(
			"/ip4/0.0.0.0/tcp/0",
			"/ip4/127.0.0.1/tcp/0",
			"/ip6/::/tcp/0",
			"/ip6/::1/tcp/0",
		),
		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.NATPortMap(),
		libp2p.EnableNATService(),
		libp2p.EnableHolePunching(),
		libp2p.EnableRelay(),
		libp2p.EnableRelayService(),
	)
	if err != nil {
		return nil, fmt.Errorf("create libp2p host: %w", err)
	}

	transport := &libp2pSyncTransport{
		app:       a,
		libraryID: local.LibraryID,
		deviceID:  local.DeviceID,
		host:      hostNode,
	}
	hostNode.SetStreamHandler(desktopSyncProtocolID, transport.handleSyncStream)
	hostNode.SetStreamHandler(desktopCheckpointProtocolID, transport.handleCheckpointStream)
	hostNode.SetStreamHandler(desktopPlaybackProtocolID, transport.handlePlaybackStream)
	hostNode.SetStreamHandler(desktopArtworkProtocolID, transport.handleArtworkStream)
	hostNode.SetStreamHandler(desktopMembershipProtocolID, transport.handleMembershipRefreshStream)
	hostNode.SetStreamHandler(desktopInviteJoinStartProtocolID, transport.handleInviteJoinStartStream)
	hostNode.SetStreamHandler(desktopInviteJoinStatusProtocolID, transport.handleInviteJoinStatusStream)
	hostNode.SetStreamHandler(desktopInviteJoinCancelProtocolID, transport.handleInviteJoinCancelStream)

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

	if err := a.touchDevicePeerID(ctx, local.DeviceID, hostNode.ID().String(), local.Device); err != nil {
		_ = transport.Close()
		return nil, fmt.Errorf("update local peer id: %w", err)
	}
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
	if err := t.host.Connect(connectCtx, *info); err != nil {
		return nil, fmt.Errorf("connect peer: %w", err)
	}

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
