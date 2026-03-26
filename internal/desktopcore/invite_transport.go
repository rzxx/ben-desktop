package desktopcore

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	tcp "github.com/libp2p/go-libp2p/p2p/transport/tcp"
	websocket "github.com/libp2p/go-libp2p/p2p/transport/websocket"
	"github.com/multiformats/go-multiaddr"
	"golang.org/x/crypto/nacl/box"
	"gorm.io/gorm"
)

const (
	desktopInviteJoinStartProtocolID  = protocol.ID("/ben/desktop/invite/start/1.0.0")
	desktopInviteJoinStatusProtocolID = protocol.ID("/ben/desktop/invite/status/1.0.0")
	desktopInviteJoinCancelProtocolID = protocol.ID("/ben/desktop/invite/cancel/1.0.0")
	memberInviteJoinStartProtocolID   = protocol.ID("/ben/member/invite/start/1.0.0")
	memberInviteJoinStatusProtocolID  = protocol.ID("/ben/member/invite/status/1.0.0")
	memberInviteJoinCancelProtocolID  = protocol.ID("/ben/member/invite/cancel/1.0.0")

	defaultInviteDiscoverTimeout = 10 * time.Second
)

type inviteJoinStartRequest struct {
	InviteCode string `json:"inviteCode"`
	DeviceID   string `json:"deviceId"`
	DeviceName string `json:"deviceName"`
	PeerID     string `json:"peerId"`
	JoinPubKey []byte `json:"joinPubKey"`
}

type inviteJoinStartResponse struct {
	LibraryID     string `json:"libraryId"`
	RequestID     string `json:"requestId"`
	Status        string `json:"status"`
	Message       string `json:"message"`
	Role          string `json:"role"`
	OwnerDeviceID string `json:"ownerDeviceId"`
	OwnerRole     string `json:"ownerRole"`
	OwnerPeerID   string `json:"ownerPeerId"`
	PeerAddrHint  string `json:"peerAddrHint"`
	ExpiresAt     int64  `json:"expiresAt"`
	Error         string `json:"error,omitempty"`
}

type inviteJoinStatusRequest struct {
	LibraryID string `json:"libraryId"`
	RequestID string `json:"requestId"`
	DeviceID  string `json:"deviceId"`
	PeerID    string `json:"peerId"`
}

type inviteJoinStatusResponse struct {
	LibraryID         string `json:"libraryId"`
	RequestID         string `json:"requestId"`
	Status            string `json:"status"`
	Message           string `json:"message"`
	Role              string `json:"role"`
	OwnerDeviceID     string `json:"ownerDeviceId"`
	OwnerRole         string `json:"ownerRole"`
	OwnerPeerID       string `json:"ownerPeerId"`
	OwnerFingerprint  string `json:"ownerFingerprint"`
	EncryptedMaterial []byte `json:"encryptedMaterial,omitempty"`
	ExpiresAt         int64  `json:"expiresAt"`
	UpdatedAt         int64  `json:"updatedAt"`
	Error             string `json:"error,omitempty"`
}

type inviteJoinCancelRequest struct {
	LibraryID string `json:"libraryId"`
	RequestID string `json:"requestId"`
	DeviceID  string `json:"deviceId"`
	PeerID    string `json:"peerId"`
	Reason    string `json:"reason"`
}

type inviteJoinCancelResponse struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	UpdatedAt int64  `json:"updatedAt"`
	Error     string `json:"error,omitempty"`
}

type inviteJoinKeypair struct {
	PublicKey  string `json:"publicKey"`
	PrivateKey string `json:"privateKey"`
}

type inviteClientTransport struct {
	app        *App
	host       host.Host
	mdns       transportMDNSService
	serviceTag string
}

func generateInviteJoinKeypair() (*[32]byte, *[32]byte, error) {
	publicKey, privateKey, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate invite join keypair: %w", err)
	}
	return publicKey, privateKey, nil
}

func encodeInviteJoinKeypair(publicKey, privateKey *[32]byte) (string, error) {
	if publicKey == nil || privateKey == nil {
		return "", fmt.Errorf("invite join keypair is required")
	}
	body, err := json.Marshal(inviteJoinKeypair{
		PublicKey:  base64.StdEncoding.EncodeToString(publicKey[:]),
		PrivateKey: base64.StdEncoding.EncodeToString(privateKey[:]),
	})
	if err != nil {
		return "", fmt.Errorf("encode invite join keypair: %w", err)
	}
	return string(body), nil
}

func decodeInviteJoinKeypair(encoded string) (*[32]byte, *[32]byte, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, nil, fmt.Errorf("invite join keypair is required")
	}
	var body inviteJoinKeypair
	if err := json.Unmarshal([]byte(encoded), &body); err != nil {
		return nil, nil, fmt.Errorf("decode invite join keypair: %w", err)
	}
	publicRaw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(body.PublicKey))
	if err != nil {
		return nil, nil, fmt.Errorf("decode invite join public key: %w", err)
	}
	privateRaw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(body.PrivateKey))
	if err != nil {
		return nil, nil, fmt.Errorf("decode invite join private key: %w", err)
	}
	if len(publicRaw) != 32 || len(privateRaw) != 32 {
		return nil, nil, fmt.Errorf("invalid invite join keypair size")
	}
	var publicKey [32]byte
	var privateKey [32]byte
	copy(publicKey[:], publicRaw)
	copy(privateKey[:], privateRaw)
	return &publicKey, &privateKey, nil
}

func encryptJoinSessionMaterial(joinPubKey []byte, material joinSessionMaterial) ([]byte, error) {
	if len(joinPubKey) != 32 {
		return nil, fmt.Errorf("join public key must be 32 bytes")
	}
	raw, err := json.Marshal(material)
	if err != nil {
		return nil, fmt.Errorf("marshal join session material: %w", err)
	}
	var recipient [32]byte
	copy(recipient[:], joinPubKey)
	sealed, err := box.SealAnonymous(nil, raw, &recipient, rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("encrypt join session material: %w", err)
	}
	return sealed, nil
}

func decryptJoinSessionMaterial(ciphertext []byte, publicKey, privateKey *[32]byte) (joinSessionMaterial, error) {
	if len(ciphertext) == 0 {
		return joinSessionMaterial{}, fmt.Errorf("encrypted join session material is required")
	}
	if publicKey == nil || privateKey == nil {
		return joinSessionMaterial{}, fmt.Errorf("invite join keypair is required")
	}
	opened, ok := box.OpenAnonymous(nil, ciphertext, publicKey, privateKey)
	if !ok {
		return joinSessionMaterial{}, fmt.Errorf("decrypt join session material")
	}
	var material joinSessionMaterial
	if err := json.Unmarshal(opened, &material); err != nil {
		return joinSessionMaterial{}, fmt.Errorf("decode join session material: %w", err)
	}
	return material, nil
}

func joinSessionKeypairLocalSettingKey(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	return "join_session_keypair:" + sessionID
}

func saveJoinSessionKeypairTx(tx *gorm.DB, sessionID string, publicKey, privateKey *[32]byte, updatedAt time.Time) error {
	key, err := encodeInviteJoinKeypair(publicKey, privateKey)
	if err != nil {
		return err
	}
	return upsertLocalSettingTx(tx, joinSessionKeypairLocalSettingKey(sessionID), key, updatedAt)
}

func loadJoinSessionKeypair(ctx context.Context, db *gorm.DB, sessionID string) (*[32]byte, *[32]byte, bool, error) {
	key := joinSessionKeypairLocalSettingKey(sessionID)
	if strings.TrimSpace(key) == "" {
		return nil, nil, false, nil
	}
	var setting LocalSetting
	if err := db.WithContext(ctx).Where("key = ?", key).Take(&setting).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil, false, nil
		}
		return nil, nil, false, err
	}
	publicKey, privateKey, err := decodeInviteJoinKeypair(setting.Value)
	if err != nil {
		return nil, nil, false, err
	}
	return publicKey, privateKey, true, nil
}

func deleteJoinSessionKeypair(ctx context.Context, db *gorm.DB, sessionID string) error {
	key := joinSessionKeypairLocalSettingKey(sessionID)
	if strings.TrimSpace(key) == "" {
		return nil
	}
	return db.WithContext(ctx).Where("key = ?", key).Delete(&LocalSetting{}).Error
}

func (a *App) openInviteClientTransport(serviceTag string) (*inviteClientTransport, error) {
	serviceTag = strings.TrimSpace(serviceTag)
	if serviceTag == "" {
		return nil, fmt.Errorf("invite service tag is required")
	}
	priv, err := loadOrCreateTransportIdentityKey(a.cfg.IdentityKeyPath)
	if err != nil {
		return nil, fmt.Errorf("load transport identity: %w", err)
	}
	hostNode, err := libp2p.New(
		libp2p.Identity(priv),
		libp2p.ListenAddrStrings(
			"/ip4/0.0.0.0/tcp/0",
			"/ip4/0.0.0.0/tcp/0/ws",
			"/ip4/127.0.0.1/tcp/0",
			"/ip4/127.0.0.1/tcp/0/ws",
			"/ip6/::/tcp/0",
			"/ip6/::/tcp/0/ws",
			"/ip6/::1/tcp/0",
			"/ip6/::1/tcp/0/ws",
		),
		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.Transport(websocket.New),
		libp2p.Security(noise.ID, noise.New),
		libp2p.Muxer(yamux.ID, yamux.DefaultTransport),
		libp2p.NATPortMap(),
		libp2p.EnableNATService(),
		libp2p.EnableHolePunching(),
		libp2p.EnableRelay(),
		libp2p.EnableRelayService(),
	)
	if err != nil {
		return nil, fmt.Errorf("create invite client host: %w", err)
	}

	client := &inviteClientTransport{
		app:        a,
		host:       hostNode,
		serviceTag: serviceTag,
	}
	service := mdns.NewMdnsService(hostNode, serviceTag, &desktopMDNSNotifee{
		host:   hostNode,
		logger: a.cfg.Logger,
	})
	if err := service.Start(); err != nil {
		if a.cfg.Logger != nil {
			a.cfg.Logger.Errorf("desktopcore: start invite mdns failed for %s: %v", serviceTag, err)
		}
	} else {
		client.mdns = service
	}
	return client, nil
}

func (c *inviteClientTransport) Close() error {
	if c == nil {
		return nil
	}
	if c.mdns != nil {
		_ = c.mdns.Close()
	}
	if c.host != nil {
		return c.host.Close()
	}
	return nil
}

func (c *inviteClientTransport) peerAddr(peerID peer.ID) string {
	if c == nil || c.host == nil || peerID == "" {
		return ""
	}
	info := c.host.Peerstore().PeerInfo(peerID)
	for _, addr := range info.Addrs {
		return fmt.Sprintf("%s/p2p/%s", addr.String(), peerID.String())
	}
	return ""
}

func (c *inviteClientTransport) resolvePeer(ctx context.Context, peerAddrHint, expectedPeerID string) (peer.ID, string, error) {
	if c == nil || c.host == nil {
		return "", "", fmt.Errorf("invite client transport is not running")
	}
	expectedPeerID = strings.TrimSpace(expectedPeerID)
	peerAddrHint = strings.TrimSpace(peerAddrHint)

	var firstErr error
	if peerAddrHint != "" {
		ma, err := multiaddr.NewMultiaddr(peerAddrHint)
		if err != nil {
			firstErr = fmt.Errorf("parse invite peer address: %w", err)
		} else {
			info, err := peer.AddrInfoFromP2pAddr(ma)
			if err != nil {
				firstErr = fmt.Errorf("invite peer info from addr: %w", err)
			} else if expectedPeerID != "" && info.ID.String() != expectedPeerID {
				firstErr = fmt.Errorf("invite peer hint mismatch")
			} else {
				connectCtx, cancel := context.WithTimeout(ctx, transportConnectTimeout)
				err = c.host.Connect(connectCtx, *info)
				cancel()
				if err == nil {
					return info.ID, c.peerAddr(info.ID), nil
				}
				firstErr = fmt.Errorf("connect invite peer: %w", err)
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			if firstErr != nil {
				return "", "", firstErr
			}
			return "", "", ctx.Err()
		default:
		}

		connected := append([]peer.ID(nil), c.host.Network().Peers()...)
		for _, candidate := range connected {
			if expectedPeerID != "" && candidate.String() != expectedPeerID {
				continue
			}
			return candidate, c.peerAddr(candidate), nil
		}
		if expectedPeerID == "" && len(connected) > 0 {
			return connected[0], c.peerAddr(connected[0]), nil
		}

		time.Sleep(100 * time.Millisecond)
	}
}

func (c *inviteClientTransport) roundTrip(ctx context.Context, peerAddrHint, expectedPeerID string, protocolID protocol.ID, req any, resp any) (string, string, error) {
	peerID, resolvedAddr, err := c.resolvePeer(ctx, peerAddrHint, expectedPeerID)
	if err != nil {
		return "", "", err
	}
	stream, err := c.host.NewStream(ctx, peerID, protocolID)
	if err != nil {
		return "", "", fmt.Errorf("open invite stream: %w", err)
	}
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(transportStreamTimeout))

	if err := json.NewEncoder(stream).Encode(req); err != nil {
		return "", "", fmt.Errorf("write invite request: %w", err)
	}
	if err := json.NewDecoder(stream).Decode(resp); err != nil {
		return "", "", fmt.Errorf("read invite response: %w", err)
	}
	return peerID.String(), resolvedAddr, nil
}
