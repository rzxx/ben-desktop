package main

import (
	"ben/registryauth"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	_ "modernc.org/sqlite"
)

type membershipFixture struct {
	libraryID string
	deviceID  string
	peerID    string
	rootPub   string
	rootPriv  string
	authPriv  string
	auth      registryauth.TransportPeerAuth
}

func TestRegistryAuthEnforcement(t *testing.T) {
	t.Parallel()

	server := openTestRelaydServer(t)
	member := newMembershipFixture(t, "lib-1", "device-1", "peer-1")

	announce := registryauth.PresenceAnnounceRequest{
		Record: registryauth.PresenceRecord{
			LibraryID: member.libraryID,
			DeviceID:  member.deviceID,
			PeerID:    member.peerID,
			Addrs:     []string{"/ip4/203.0.113.10/tcp/4101/p2p/" + member.peerID},
		},
		RootPublicKey: member.rootPub,
		Auth:          member.auth,
	}
	if status := serveJSON(t, server.handlePresenceAnnounce, announce); status != http.StatusOK {
		t.Fatalf("presence announce status = %d", status)
	}

	if status := serveJSON(t, server.handlePresenceMember, registryauth.MemberLookupRequest{
		LibraryID: member.libraryID,
		PeerID:    member.peerID,
	}); status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated member lookup status = %d", status)
	}

	recorder := httptest.NewRecorder()
	request := jsonRequest(t, http.MethodPost, "/v1/presence/member", registryauth.MemberLookupRequest{
		LibraryID:     member.libraryID,
		PeerID:        member.peerID,
		RootPublicKey: member.rootPub,
		Auth:          member.auth,
	})
	server.handlePresenceMember(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("authenticated member lookup status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var record presenceRecord
	if err := json.Unmarshal(recorder.Body.Bytes(), &record); err != nil {
		t.Fatalf("decode presence record: %v", err)
	}
	if record.PeerID != member.peerID {
		t.Fatalf("presence record peer = %q", record.PeerID)
	}

	if status := serveJSON(t, server.handleInviteOwner, registryauth.InviteOwnerLookupRequest{
		Invite: registryauth.InviteAttestation{
			LibraryID:     member.libraryID,
			TokenID:       "token-1",
			OwnerPeerID:   member.peerID,
			RootPublicKey: member.rootPub,
		},
	}); status != http.StatusUnauthorized {
		t.Fatalf("unsigned invite lookup status = %d", status)
	}

	attestation, err := registryauth.SignInviteAttestation(registryauth.InviteAttestation{
		LibraryID:     member.libraryID,
		TokenID:       "token-1",
		OwnerPeerID:   member.peerID,
		RootPublicKey: member.rootPub,
		ExpiresAt:     time.Now().Add(time.Hour).Unix(),
	}, member.rootPriv)
	if err != nil {
		t.Fatalf("sign invite attestation: %v", err)
	}
	if status := serveJSON(t, server.handleInviteOwner, registryauth.InviteOwnerLookupRequest{Invite: attestation}); status != http.StatusOK {
		t.Fatalf("signed invite lookup status = %d", status)
	}
}

func TestInviteOwnerRejectsExpiredAttestation(t *testing.T) {
	t.Parallel()

	server := openTestRelaydServer(t)
	member := newMembershipFixture(t, "lib-1", "device-1", "peer-1")

	announce := registryauth.PresenceAnnounceRequest{
		Record: registryauth.PresenceRecord{
			LibraryID: member.libraryID,
			DeviceID:  member.deviceID,
			PeerID:    member.peerID,
			Addrs:     []string{"/ip4/203.0.113.10/tcp/4101/p2p/" + member.peerID},
		},
		RootPublicKey: member.rootPub,
		Auth:          member.auth,
	}
	if status := serveJSON(t, server.handlePresenceAnnounce, announce); status != http.StatusOK {
		t.Fatalf("presence announce status = %d", status)
	}

	expiredInvite, err := registryauth.SignInviteAttestation(registryauth.InviteAttestation{
		LibraryID:     member.libraryID,
		TokenID:       "token-expired",
		OwnerPeerID:   member.peerID,
		RootPublicKey: member.rootPub,
		ExpiresAt:     time.Now().Add(-time.Minute).Unix(),
	}, member.rootPriv)
	if err != nil {
		t.Fatalf("sign expired invite attestation: %v", err)
	}

	if status := serveJSON(t, server.handleInviteOwner, registryauth.InviteOwnerLookupRequest{Invite: expiredInvite}); status != http.StatusUnauthorized {
		t.Fatalf("expired invite lookup status = %d", status)
	}
}

func TestPresenceAnnounceRejectsPinnedRootMismatch(t *testing.T) {
	t.Parallel()

	server := openTestRelaydServer(t)
	member := newMembershipFixture(t, "lib-1", "device-1", "peer-1")

	if status := serveJSON(t, server.handlePresenceAnnounce, registryauth.PresenceAnnounceRequest{
		Record: registryauth.PresenceRecord{
			LibraryID: member.libraryID,
			DeviceID:  member.deviceID,
			PeerID:    member.peerID,
			Addrs:     []string{"/ip4/203.0.113.10/tcp/4101/p2p/" + member.peerID},
		},
		RootPublicKey: member.rootPub,
		Auth:          member.auth,
	}); status != http.StatusOK {
		t.Fatalf("initial presence announce status = %d", status)
	}

	otherRoot := newMembershipFixture(t, member.libraryID, "device-2", "peer-2")
	if status := serveJSON(t, server.handlePresenceAnnounce, registryauth.PresenceAnnounceRequest{
		Record: registryauth.PresenceRecord{
			LibraryID: otherRoot.libraryID,
			DeviceID:  otherRoot.deviceID,
			PeerID:    otherRoot.peerID,
			Addrs:     []string{"/ip4/203.0.113.11/tcp/4101/p2p/" + otherRoot.peerID},
		},
		RootPublicKey: otherRoot.rootPub,
		Auth:          otherRoot.auth,
	}); status != http.StatusUnauthorized {
		t.Fatalf("pinned-root mismatch announce status = %d", status)
	}
}

func TestPresenceAnnounceRejectsStaleMembershipSerial(t *testing.T) {
	t.Parallel()

	server := openTestRelaydServer(t)
	member := newMembershipFixture(t, "lib-1", "device-1", "peer-1")
	newerAuth := member.auth
	newerAuth.Cert = signedMembershipCert(t, member, 2, member.peerID, time.Now().Add(time.Hour))

	if status := serveJSON(t, server.handlePresenceAnnounce, registryauth.PresenceAnnounceRequest{
		Record: registryauth.PresenceRecord{
			LibraryID: member.libraryID,
			DeviceID:  member.deviceID,
			PeerID:    member.peerID,
			Addrs:     []string{"/ip4/203.0.113.10/tcp/4101/p2p/" + member.peerID},
		},
		RootPublicKey: member.rootPub,
		Auth:          newerAuth,
	}); status != http.StatusOK {
		t.Fatalf("newer membership announce status = %d", status)
	}

	if status := serveJSON(t, server.handlePresenceAnnounce, registryauth.PresenceAnnounceRequest{
		Record: registryauth.PresenceRecord{
			LibraryID: member.libraryID,
			DeviceID:  member.deviceID,
			PeerID:    member.peerID,
			Addrs:     []string{"/ip4/203.0.113.10/tcp/4101/p2p/" + member.peerID},
		},
		RootPublicKey: member.rootPub,
		Auth:          member.auth,
	}); status != http.StatusUnauthorized {
		t.Fatalf("stale membership announce status = %d", status)
	}
}

func TestLoadOrCreateRelayIdentityKeyPersistsPeerID(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "relay", "identity.key")
	first, err := loadOrCreateRelayIdentityKey(path)
	if err != nil {
		t.Fatalf("load first relay identity: %v", err)
	}
	second, err := loadOrCreateRelayIdentityKey(path)
	if err != nil {
		t.Fatalf("load second relay identity: %v", err)
	}

	firstPeerID, err := peer.IDFromPrivateKey(first)
	if err != nil {
		t.Fatalf("first peer id: %v", err)
	}
	secondPeerID, err := peer.IDFromPrivateKey(second)
	if err != nil {
		t.Fatalf("second peer id: %v", err)
	}
	if firstPeerID != secondPeerID {
		t.Fatalf("persisted relay peer id mismatch: first=%s second=%s", firstPeerID, secondPeerID)
	}
}

func TestParseOptionsRejectsPartialTLSConfig(t *testing.T) {
	t.Parallel()

	_, err := parseOptionsFromArgs([]string{"-tls-cert", "cert.pem"})
	if err == nil || !strings.Contains(err.Error(), "tls key path is required") {
		t.Fatalf("partial tls config error = %v", err)
	}
}

func TestParseOptionsAcceptsDirectTLSAndRelayKnobs(t *testing.T) {
	t.Parallel()

	opts, err := parseOptionsFromArgs([]string{
		"-http-addr", ":9443",
		"-identity-key", "relay.key",
		"-tls-cert", "cert.pem",
		"-tls-key", "key.pem",
		"-peer-listen-addrs", "/ip4/0.0.0.0/tcp/4101,/ip4/0.0.0.0/udp/4101/quic-v1",
		"-advertise-addrs", "/dns4/relay.example.com/tcp/15140",
		"-rate-limit-rps", "25",
		"-rate-limit-burst", "50",
		"-max-body-bytes", "2048",
		"-relay-max-circuits", "12",
	})
	if err != nil {
		t.Fatalf("parse options: %v", err)
	}
	if !opts.TLSEnabled() {
		t.Fatal("expected tls to be enabled")
	}
	if got := strings.Join(opts.PeerListenAddrs, ","); got != "/ip4/0.0.0.0/tcp/4101,/ip4/0.0.0.0/udp/4101/quic-v1" {
		t.Fatalf("peer listen addrs = %q", got)
	}
	if got := strings.Join(opts.AdvertiseAddrs, ","); got != "/dns4/relay.example.com/tcp/15140" {
		t.Fatalf("advertise addrs = %q", got)
	}
	if opts.RateLimitBurst != 50 || opts.MaxBodyBytes != 2048 || opts.MaxCircuits != 12 {
		t.Fatalf("parsed knobs mismatch: %+v", opts)
	}
}

func TestParseOptionsUsesRailwayFriendlyEnvDefaults(t *testing.T) {
	t.Setenv(envPort, "8788")
	t.Setenv(envDBPath, "/data/registry.db")
	t.Setenv(envIdentityKeyPath, "/data/identity.key")
	t.Setenv(envPeerListenAddrs, "/ip4/0.0.0.0/tcp/4001")
	t.Setenv(envAdvertiseAddrs, "/dns4/relay-p2p.example.com/tcp/15140")
	t.Setenv(envTLSCertPath, "/tls/fullchain.pem")
	t.Setenv(envTLSKeyPath, "/tls/privkey.pem")

	opts, err := parseOptionsFromArgs(nil)
	if err != nil {
		t.Fatalf("parse options from env: %v", err)
	}
	if opts.HTTPAddr != ":8788" {
		t.Fatalf("http addr = %q", opts.HTTPAddr)
	}
	if opts.DBPath != "/data/registry.db" || opts.IdentityKeyPath != "/data/identity.key" {
		t.Fatalf("storage paths = %+v", opts)
	}
	if got := strings.Join(opts.PeerListenAddrs, ","); got != "/ip4/0.0.0.0/tcp/4001" {
		t.Fatalf("peer listen addrs = %q", got)
	}
	if got := strings.Join(opts.AdvertiseAddrs, ","); got != "/dns4/relay-p2p.example.com/tcp/15140" {
		t.Fatalf("advertise addrs = %q", got)
	}
	if !opts.TLSEnabled() {
		t.Fatal("expected tls to be enabled from env")
	}
}

func TestParseOptionsRejectsInvalidAdvertiseAddr(t *testing.T) {
	t.Parallel()

	_, err := parseOptionsFromArgs([]string{"-advertise-addrs", "not-a-multiaddr"})
	if err == nil || !strings.Contains(err.Error(), "parse advertise address") {
		t.Fatalf("invalid advertise addr error = %v", err)
	}
}

func TestNewRelayHostUsesExplicitAdvertiseAddrs(t *testing.T) {
	t.Parallel()

	opts := relaydOptions{
		HTTPAddr:               ":8787",
		DBPath:                 filepath.Join(t.TempDir(), "registry.db"),
		IdentityKeyPath:        filepath.Join(t.TempDir(), "identity.key"),
		PeerListenAddrs:        []string{"/ip4/127.0.0.1/tcp/0"},
		AdvertiseAddrs:         []string{"/dns4/relay-p2p.example.com/tcp/15140"},
		ReadHeaderTimeout:      time.Second,
		ReadTimeout:            time.Second,
		WriteTimeout:           time.Second,
		IdleTimeout:            time.Second,
		ShutdownTimeout:        time.Second,
		MaxBodyBytes:           defaultMaxBodyBytes,
		RateLimitIdleTTL:       time.Minute,
		RateLimitBurst:         1,
		ReservationTTL:         time.Hour,
		MaxReservations:        1,
		MaxCircuits:            1,
		MaxReservationsPerPeer: 1,
		MaxReservationsPerIP:   1,
		MaxReservationsPerASN:  1,
		RelayLimitDuration:     time.Second,
		RelayLimitDataBytes:    1024,
	}
	hostNode, err := newRelayHost(opts)
	if err != nil {
		t.Fatalf("new relay host: %v", err)
	}
	defer hostNode.Close()

	addrs := formatHostAddrs(hostNode)
	if len(addrs) != 1 {
		t.Fatalf("advertised addrs count = %d (%v)", len(addrs), addrs)
	}
	if !strings.Contains(addrs[0], "/dns4/relay-p2p.example.com/tcp/15140/p2p/") {
		t.Fatalf("advertised addr = %q", addrs[0])
	}
}

func TestRateLimitMiddlewareRejectsBurstExceededRequests(t *testing.T) {
	t.Parallel()

	limiter := newIPRateLimiter(1, 1, time.Minute)
	handler := rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), limiter)

	req1 := httptest.NewRequest(http.MethodGet, "/v1/presence/member", nil)
	req1.RemoteAddr = "203.0.113.10:1234"
	resp1 := httptest.NewRecorder()
	handler.ServeHTTP(resp1, req1)
	if resp1.Code != http.StatusOK {
		t.Fatalf("first response = %d", resp1.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/v1/presence/member", nil)
	req2.RemoteAddr = "203.0.113.10:1235"
	resp2 := httptest.NewRecorder()
	handler.ServeHTTP(resp2, req2)
	if resp2.Code != http.StatusTooManyRequests {
		t.Fatalf("second response = %d", resp2.Code)
	}
}

func TestRateLimiterCleanupDropsIdleVisitors(t *testing.T) {
	t.Parallel()

	limiter := newIPRateLimiter(1, 1, time.Minute)
	if !limiter.Allow("203.0.113.10") {
		t.Fatal("expected initial allow")
	}
	limiter.cleanup(time.Now().UTC().Add(2 * time.Minute))
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if len(limiter.visitors) != 0 {
		t.Fatalf("visitor count = %d", len(limiter.visitors))
	}
}

func TestMaxBodyBytesMiddlewareRejectsOversizeRequests(t *testing.T) {
	t.Parallel()

	handler, _ := buildHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]string
		if !decodeJSON(w, r, &payload) {
			return
		}
		w.WriteHeader(http.StatusOK)
	}), relaydOptions{
		MaxBodyBytes:               32,
		RateLimitRequestsPerSecond: 0,
		ReadHeaderTimeout:          time.Second,
		ReadTimeout:                time.Second,
		WriteTimeout:               time.Second,
		IdleTimeout:                time.Second,
		ShutdownTimeout:            time.Second,
		ReservationTTL:             time.Hour,
		MaxReservations:            1,
		MaxCircuits:                1,
		MaxReservationsPerPeer:     1,
		MaxReservationsPerIP:       1,
		MaxReservationsPerASN:      1,
		RelayLimitDuration:         time.Second,
		RelayLimitDataBytes:        1024,
	})

	body := `{"payload":"abcdefghijklmnopqrstuvwxyz"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/presence/member", strings.NewReader(body))
	req.RemoteAddr = "203.0.113.10:1234"
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("response code = %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestHealthzBypassesRateLimit(t *testing.T) {
	t.Parallel()

	limiter := newIPRateLimiter(1, 1, time.Minute)
	handler := rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), limiter)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "203.0.113.10:1234"
	for i := 0; i < 3; i++ {
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("healthz attempt %d status = %d", i, resp.Code)
		}
	}
}

func TestRateLimiterCleanupLoopStopsWithContext(t *testing.T) {
	t.Parallel()

	limiter := newIPRateLimiter(1, 1, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		limiter.cleanupLoop(ctx)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup loop did not stop")
	}
}

func openTestRelaydServer(t *testing.T) *relaydServer {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		_ = db.Close()
	})
	if err := initSchema(db); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	return &relaydServer{db: db}
}

func newMembershipFixture(t *testing.T, libraryID, deviceID, peerID string) membershipFixture {
	t.Helper()
	rootPub, rootPriv := generateKeyPair(t)
	authorityPub, authorityPriv := generateKeyPair(t)
	now := time.Now().UTC()
	authority := registryauth.AdmissionAuthorityEnvelope{
		Version:      1,
		PublicKey:    authorityPub,
		PrevVersion:  0,
		SignedByKind: registryauth.AdmissionAuthoritySignedByRoot,
		CreatedAt:    now.UnixNano(),
	}
	authorityPayload, err := registryauth.AdmissionAuthoritySigningPayload(libraryID, authority)
	if err != nil {
		t.Fatalf("authority signing payload: %v", err)
	}
	rootPrivateKey, err := registryauth.DecodeEd25519PrivateKey(rootPriv)
	if err != nil {
		t.Fatalf("decode root private key: %v", err)
	}
	authority.Sig = ed25519.Sign(ed25519.PrivateKey(rootPrivateKey), authorityPayload)

	return membershipFixture{
		libraryID: libraryID,
		deviceID:  deviceID,
		peerID:    peerID,
		rootPub:   rootPub,
		rootPriv:  rootPriv,
		authPriv:  authorityPriv,
		auth: registryauth.TransportPeerAuth{
			Cert:           signedMembershipCert(t, membershipFixture{libraryID: libraryID, deviceID: deviceID, peerID: peerID, authPriv: authorityPriv}, 1, peerID, now.Add(time.Hour)),
			AuthorityChain: []registryauth.AdmissionAuthorityEnvelope{authority},
		},
	}
}

func signedMembershipCert(t *testing.T, member membershipFixture, serial int64, peerID string, expiresAt time.Time) registryauth.MembershipCertEnvelope {
	t.Helper()
	cert := registryauth.MembershipCertEnvelope{
		LibraryID:        member.libraryID,
		DeviceID:         member.deviceID,
		PeerID:           peerID,
		Role:             "member",
		AuthorityVersion: 1,
		Serial:           serial,
		IssuedAt:         time.Now().UTC().UnixNano(),
		ExpiresAt:        expiresAt.UTC().UnixNano(),
	}
	certPayload, err := registryauth.MembershipCertSigningPayload(cert)
	if err != nil {
		t.Fatalf("membership signing payload: %v", err)
	}
	authorityPrivateKey, err := registryauth.DecodeEd25519PrivateKey(member.authPriv)
	if err != nil {
		t.Fatalf("decode authority private key: %v", err)
	}
	cert.Sig = ed25519.Sign(ed25519.PrivateKey(authorityPrivateKey), certPayload)
	return cert
}

func generateKeyPair(t *testing.T) (string, string) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}
	return base64.StdEncoding.EncodeToString(publicKey), base64.StdEncoding.EncodeToString(privateKey)
}

func serveJSON(t *testing.T, handler http.HandlerFunc, body any) int {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler(recorder, jsonRequest(t, http.MethodPost, "/", body))
	return recorder.Code
}

func jsonRequest(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	return request
}
