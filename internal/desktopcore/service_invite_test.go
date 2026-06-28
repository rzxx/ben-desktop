package desktopcore

import (
	"ben/registryauth"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	apitypes "ben/desktop/api/types"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	relayv2 "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
)

type inviteTestInfra struct {
	registryURL         string
	relayBootstrapAddrs []string
	registry            *inviteTestRegistry
	relayHost           host.Host
}

type inviteTestRegistry struct {
	mu          sync.Mutex
	records     map[string]registryauth.PresenceRecord
	relayPeerID string
	relayAddrs  []string
	revocations []registryauth.RevocationSyncRequest
}

func openInviteTestInfra(t *testing.T) *inviteTestInfra {
	t.Helper()

	relayHost := openInviteTestRelayHost(t)
	addrs := inviteTestRelayBootstrapAddrs(relayHost)

	registry := &inviteTestRegistry{
		records:     make(map[string]registryauth.PresenceRecord),
		relayPeerID: relayHost.ID().String(),
		relayAddrs:  compactNonEmptyStrings(addrs),
	}
	server := httptest.NewServer(http.HandlerFunc(registry.serveHTTP))
	t.Cleanup(server.Close)

	infra := &inviteTestInfra{
		registryURL:         server.URL,
		relayBootstrapAddrs: compactNonEmptyStrings(addrs),
		registry:            registry,
		relayHost:           relayHost,
	}
	t.Cleanup(func() {
		if infra.relayHost != nil {
			_ = infra.relayHost.Close()
		}
	})
	return infra
}

func openInviteTestRelayHost(t *testing.T) host.Host {
	t.Helper()
	relayHost, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0", "/ip4/127.0.0.1/udp/0/quic-v1"),
		libp2p.ForceReachabilityPublic(),
		libp2p.EnableRelayService(relayv2.WithResources(newRelayResources())),
	)
	if err != nil {
		t.Fatalf("open relay host: %v", err)
	}
	return relayHost
}

func inviteTestRelayBootstrapAddrs(relayHost host.Host) []string {
	if relayHost == nil {
		return nil
	}
	addrs := make([]string, 0, len(relayHost.Addrs()))
	for _, addr := range relayHost.Addrs() {
		addrs = append(addrs, fmt.Sprintf("%s/p2p/%s", addr.String(), relayHost.ID().String()))
	}
	return compactNonEmptyStrings(addrs)
}

func (i *inviteTestInfra) replaceRelay(t *testing.T) {
	t.Helper()
	if i == nil || i.registry == nil {
		t.Fatal("invite test infrastructure is not configured")
	}
	next := openInviteTestRelayHost(t)
	addrs := inviteTestRelayBootstrapAddrs(next)
	previous := i.relayHost
	i.relayHost = next
	i.relayBootstrapAddrs = append([]string(nil), addrs...)
	i.registry.mu.Lock()
	i.registry.relayPeerID = next.ID().String()
	i.registry.relayAddrs = append([]string(nil), addrs...)
	i.registry.records = make(map[string]registryauth.PresenceRecord)
	i.registry.revocations = nil
	i.registry.mu.Unlock()
	if previous != nil {
		_ = previous.Close()
	}
}

func configureInviteTestApp(app *App, infra *inviteTestInfra) {
	if app == nil || infra == nil {
		return
	}
	app.cfg.RegistryURL = strings.TrimSpace(infra.registryURL)
	app.cfg.RelayBootstrapAddrs = append([]string(nil), infra.relayBootstrapAddrs...)
	app.cfg.EnableLANDiscovery = false
	app.cfg.enableLANDiscoverySet = true
}

func (r *inviteTestRegistry) serveHTTP(w http.ResponseWriter, req *http.Request) {
	switch req.URL.Path {
	case "/v1/presence/announce":
		r.handlePresenceAnnounce(w, req)
	case "/v1/presence/member":
		r.handlePresenceMember(w, req)
	case "/v1/relay/authorize":
		r.handleRelayAuthorize(w, req)
	case "/v1/revocations/sync":
		r.handleRevocationSync(w, req)
	case "/v1/invites/owner":
		r.handleInviteOwner(w, req)
	case "/healthz":
		r.handleHealthz(w, req)
	default:
		http.NotFound(w, req)
	}
}

func (r *inviteTestRegistry) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	r.mu.Lock()
	addrs := append([]string(nil), r.relayAddrs...)
	peerID := r.relayPeerID
	r.mu.Unlock()
	writeInviteRegistryJSON(w, relayHealthResponse{Addrs: addrs, PeerID: peerID})
}

func (r *inviteTestRegistry) handlePresenceAnnounce(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body registryauth.PresenceAnnounceRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		http.Error(w, fmt.Sprintf("decode announce request: %v", err), http.StatusBadRequest)
		return
	}
	record := body.Record
	record.LibraryID = strings.TrimSpace(record.LibraryID)
	record.DeviceID = strings.TrimSpace(record.DeviceID)
	record.PeerID = strings.TrimSpace(record.PeerID)
	record.Addrs = compactNonEmptyStrings(record.Addrs)
	if record.LibraryID == "" || record.PeerID == "" {
		http.Error(w, "library id and peer id are required", http.StatusBadRequest)
		return
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = time.Now().UTC()
	}
	if record.ExpiresAt.IsZero() {
		record.ExpiresAt = record.UpdatedAt.Add(90 * time.Second)
	}
	r.mu.Lock()
	r.records[inviteRegistryKey(record.LibraryID, record.PeerID)] = record
	r.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (r *inviteTestRegistry) handleRelayAuthorize(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (r *inviteTestRegistry) handleRevocationSync(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body registryauth.RevocationSyncRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		http.Error(w, fmt.Sprintf("decode revocation sync request: %v", err), http.StatusBadRequest)
		return
	}
	if err := registryauth.VerifyRevocationSync(body); err != nil {
		http.Error(w, fmt.Sprintf("verify revocation sync request: %v", err), http.StatusUnauthorized)
		return
	}
	r.mu.Lock()
	r.revocations = append(r.revocations, body)
	r.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (r *inviteTestRegistry) handlePresenceMember(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body registryauth.MemberLookupRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		http.Error(w, fmt.Sprintf("decode member lookup request: %v", err), http.StatusBadRequest)
		return
	}
	record, ok := r.lookup(body.LibraryID, body.PeerID)
	if !ok {
		http.NotFound(w, req)
		return
	}
	writeInviteRegistryJSON(w, record)
}

func (r *inviteTestRegistry) handleInviteOwner(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body registryauth.InviteOwnerLookupRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		http.Error(w, fmt.Sprintf("decode invite lookup request: %v", err), http.StatusBadRequest)
		return
	}
	if err := registryauth.VerifyInviteAttestation(body.Invite, time.Now().UTC()); err != nil {
		http.Error(w, fmt.Sprintf("verify invite lookup request: %v", err), http.StatusUnauthorized)
		return
	}
	record, ok := r.lookup(body.Invite.LibraryID, body.Invite.OwnerPeerID)
	if !ok {
		http.NotFound(w, req)
		return
	}
	record.Addrs = filterRelayInviteAddrs(record.Addrs)
	if len(record.Addrs) == 0 {
		http.NotFound(w, req)
		return
	}
	writeInviteRegistryJSON(w, record)
}

func (r *inviteTestRegistry) lookup(libraryID, peerID string) (registryauth.PresenceRecord, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	record, ok := r.records[inviteRegistryKey(libraryID, peerID)]
	if !ok {
		return registryauth.PresenceRecord{}, false
	}
	if !record.ExpiresAt.IsZero() && record.ExpiresAt.Before(time.Now().UTC()) {
		delete(r.records, inviteRegistryKey(libraryID, peerID))
		return registryauth.PresenceRecord{}, false
	}
	return record, true
}

func (r *inviteTestRegistry) clearPresence() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = make(map[string]registryauth.PresenceRecord)
}

func (r *inviteTestRegistry) revocationSyncs() []registryauth.RevocationSyncRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]registryauth.RevocationSyncRequest(nil), r.revocations...)
}

func inviteRegistryKey(libraryID, peerID string) string {
	return strings.TrimSpace(libraryID) + ":" + strings.TrimSpace(peerID)
}

func writeInviteRegistryJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(value)
}

func TestInviteCreateListRevokeActiveState(t *testing.T) {
	ctx := context.Background()
	infra := openInviteTestInfra(t)
	app := openCacheTestApp(t, 1024)
	configureInviteTestApp(app, infra)
	library, err := app.CreateLibrary(ctx, "invite-active")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}

	invite, err := app.CreateInvite(ctx, apitypes.InviteCreateRequest{})
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	if invite.LibraryID != library.LibraryID || invite.Role != roleMember || invite.Reusable {
		t.Fatalf("invite defaults = %+v", invite)
	}
	if !strings.HasPrefix(invite.InviteCode, "ben-invite-v4.") {
		t.Fatalf("invite code prefix = %q", invite.InviteCode)
	}
	if invite.ExpiresAt.Before(invite.CreatedAt.Add(23*time.Hour)) || invite.ExpiresAt.After(invite.CreatedAt.Add(25*time.Hour)) {
		t.Fatalf("single-use expiry = %v, created = %v", invite.ExpiresAt, invite.CreatedAt)
	}
	payload, err := decodeInviteCode(invite.InviteCode)
	if err != nil {
		t.Fatalf("decode invite: %v", err)
	}
	if payload.RegistryURL != infra.registryURL || len(payload.RelayBootstrapAddrs) == 0 || payload.InviteAuth == nil {
		t.Fatalf("invite payload relay/auth = %+v", payload)
	}

	active, err := app.ListActiveInvites(ctx)
	if err != nil {
		t.Fatalf("list active invites: %v", err)
	}
	if len(active) != 1 || active[0].InviteID != invite.InviteID {
		t.Fatalf("active invites = %+v", active)
	}
	if err := app.DeleteInvite(ctx, invite.InviteID); err != nil {
		t.Fatalf("delete invite: %v", err)
	}
	active, err = app.ListActiveInvites(ctx)
	if err != nil {
		t.Fatalf("list active after delete: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("active after delete = %+v", active)
	}
}

func TestSingleUseInviteAutoFinalizesAndDisappearsAfterApproval(t *testing.T) {
	ctx := context.Background()
	infra := openInviteTestInfra(t)
	owner := openCacheTestApp(t, 1024)
	configureInviteTestApp(owner, infra)
	joiner := openCacheTestApp(t, 1024)
	configureInviteTestApp(joiner, infra)

	library, err := owner.CreateLibrary(ctx, "invite-single-use")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	invite, err := owner.CreateInvite(ctx, apitypes.InviteCreateRequest{})
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	joinerDevice, err := joiner.ensureCurrentDevice(ctx)
	if err != nil {
		t.Fatalf("joiner device: %v", err)
	}

	attempt, err := joiner.StartJoinFromInvite(ctx, apitypes.JoinFromInviteInput{
		InviteCode: invite.InviteCode,
		DeviceName: "Joiner",
	})
	if err != nil {
		t.Fatalf("start join: %v", err)
	}
	request := waitForPendingJoinRequest(t, ctx, owner, attempt.RequestID)
	if request.DeviceID != joinerDevice.DeviceID || request.Role != roleMember {
		t.Fatalf("pending request = %+v", request)
	}

	if err := owner.ApproveJoinRequest(ctx, attempt.RequestID); err != nil {
		t.Fatalf("approve join request: %v", err)
	}
	if requests, err := owner.ListJoinRequests(ctx); err != nil || len(requests) != 0 {
		t.Fatalf("visible requests after approval = %+v err=%v", requests, err)
	}
	completed := waitForJoinAttemptStatus(t, ctx, joiner, attempt.AttemptID, inviteJoinStatusCompleted)
	if completed.Role != roleMember || completed.LibraryID != library.LibraryID {
		t.Fatalf("completed attempt = %+v", completed)
	}
	active, err := owner.ListActiveInvites(ctx)
	if err != nil {
		t.Fatalf("list active invites: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("single-use invite still active = %+v", active)
	}

	local, err := joiner.EnsureLocalContext(ctx)
	if err != nil {
		t.Fatalf("joiner local context: %v", err)
	}
	if local.LibraryID != library.LibraryID {
		t.Fatalf("active library = %q, want %q", local.LibraryID, library.LibraryID)
	}
	var membership Membership
	if err := joiner.db.WithContext(ctx).
		Where("library_id = ? AND device_id = ?", library.LibraryID, joinerDevice.DeviceID).
		Take(&membership).Error; err != nil {
		t.Fatalf("load joined membership: %v", err)
	}
	if membership.Role != roleMember {
		t.Fatalf("membership role = %q, want %q", membership.Role, roleMember)
	}
}

func TestReusableInviteSurvivesApprovalsUntilRevoked(t *testing.T) {
	ctx := context.Background()
	infra := openInviteTestInfra(t)
	owner := openCacheTestApp(t, 1024)
	configureInviteTestApp(owner, infra)
	firstJoiner := openCacheTestApp(t, 1024)
	configureInviteTestApp(firstJoiner, infra)
	secondJoiner := openCacheTestApp(t, 1024)
	configureInviteTestApp(secondJoiner, infra)

	if _, err := owner.CreateLibrary(ctx, "invite-reusable"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	invite, err := owner.CreateInvite(ctx, apitypes.InviteCreateRequest{Role: roleGuest, Reusable: true})
	if err != nil {
		t.Fatalf("create reusable invite: %v", err)
	}
	if !invite.Reusable || !invite.ExpiresAt.IsZero() {
		t.Fatalf("reusable invite = %+v", invite)
	}

	first := startAndApproveJoin(t, ctx, owner, firstJoiner, invite.InviteCode)
	if first.Role != roleGuest {
		t.Fatalf("first joined role = %q, want %q", first.Role, roleGuest)
	}
	active, err := owner.ListActiveInvites(ctx)
	if err != nil {
		t.Fatalf("list active after first join: %v", err)
	}
	if len(active) != 1 || active[0].InviteID != invite.InviteID {
		t.Fatalf("reusable invite after first join = %+v", active)
	}

	second := startAndApproveJoin(t, ctx, owner, secondJoiner, invite.InviteCode)
	if second.Role != roleGuest {
		t.Fatalf("second joined role = %q, want %q", second.Role, roleGuest)
	}
	if err := owner.DeleteInvite(ctx, invite.InviteID); err != nil {
		t.Fatalf("revoke reusable invite: %v", err)
	}
	active, err = owner.ListActiveInvites(ctx)
	if err != nil {
		t.Fatalf("list active after revoke: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("reusable invite after revoke = %+v", active)
	}
}

func TestPendingJoinRequestsAreMemoryOnly(t *testing.T) {
	ctx := context.Background()
	infra := openInviteTestInfra(t)
	owner := openCacheTestApp(t, 1024)
	configureInviteTestApp(owner, infra)
	joiner := openCacheTestApp(t, 1024)
	configureInviteTestApp(joiner, infra)

	if _, err := owner.CreateLibrary(ctx, "invite-memory-only"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	invite, err := owner.CreateInvite(ctx, apitypes.InviteCreateRequest{})
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	attempt, err := joiner.StartJoinFromInvite(ctx, apitypes.JoinFromInviteInput{InviteCode: invite.InviteCode})
	if err != nil {
		t.Fatalf("start join: %v", err)
	}
	waitForPendingJoinRequest(t, ctx, owner, attempt.RequestID)

	owner.invite = &InviteService{app: owner}
	requests, err := owner.ListJoinRequests(ctx)
	if err != nil {
		t.Fatalf("list join requests after service restart: %v", err)
	}
	if len(requests) != 0 {
		t.Fatalf("persisted join requests = %+v", requests)
	}
}

func TestMissingRelayPresenceReturnsInviteHostUnavailable(t *testing.T) {
	ctx := context.Background()
	infra := openInviteTestInfra(t)
	owner := openCacheTestApp(t, 1024)
	configureInviteTestApp(owner, infra)
	joiner := openCacheTestApp(t, 1024)
	configureInviteTestApp(joiner, infra)

	if _, err := owner.CreateLibrary(ctx, "invite-owner-unavailable"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	invite, err := owner.CreateInvite(ctx, apitypes.InviteCreateRequest{})
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	infra.registry.clearPresence()

	_, err = joiner.StartJoinFromInvite(ctx, apitypes.JoinFromInviteInput{InviteCode: invite.InviteCode})
	if err == nil || !strings.Contains(err.Error(), "invite host unavailable") {
		t.Fatalf("start join err = %v", err)
	}
}

func TestPerLibraryRelayConfigSeedsEncodesAndRestores(t *testing.T) {
	ctx := context.Background()
	initial := openInviteTestInfra(t)
	next := openInviteTestInfra(t)
	owner := openCacheTestApp(t, 1024)
	configureInviteTestApp(owner, initial)
	joiner := openCacheTestApp(t, 1024)

	library, err := owner.CreateLibrary(ctx, "invite-library-relay")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	seeded, err := owner.GetLibraryRelayConfig(ctx, library.LibraryID)
	if err != nil {
		t.Fatalf("get seeded relay config: %v", err)
	}
	if seeded.RegistryURL != initial.registryURL || strings.Join(seeded.RelayBootstrapAddrs, "\n") != strings.Join(initial.relayBootstrapAddrs, "\n") {
		t.Fatalf("seeded relay config = %+v", seeded)
	}

	updated, err := owner.UpdateLibraryRelayConfig(ctx, apitypes.UpdateLibraryRelayConfigRequest{
		LibraryID:           library.LibraryID,
		RegistryURL:         next.registryURL,
		RelayBootstrapAddrs: next.relayBootstrapAddrs,
	})
	if err != nil {
		t.Fatalf("update relay config: %v", err)
	}
	if updated.RegistryURL != next.registryURL {
		t.Fatalf("updated relay config = %+v", updated)
	}
	invite, err := owner.CreateInvite(ctx, apitypes.InviteCreateRequest{})
	if err != nil {
		t.Fatalf("create invite with updated relay: %v", err)
	}
	payload, err := decodeInviteCode(invite.InviteCode)
	if err != nil {
		t.Fatalf("decode invite: %v", err)
	}
	if payload.RegistryURL != next.registryURL || strings.Join(payload.RelayBootstrapAddrs, "\n") != strings.Join(next.relayBootstrapAddrs, "\n") {
		t.Fatalf("encoded relay config = %+v", payload)
	}

	startAndApproveJoin(t, ctx, owner, joiner, invite.InviteCode)
	restored, err := joiner.GetLibraryRelayConfig(ctx, library.LibraryID)
	if err != nil {
		t.Fatalf("get restored relay config: %v", err)
	}
	if restored.RegistryURL != next.registryURL || strings.Join(restored.RelayBootstrapAddrs, "\n") != strings.Join(next.relayBootstrapAddrs, "\n") {
		t.Fatalf("restored relay config = %+v", restored)
	}
	status := joiner.NetworkStatus()
	if status.RegistryURL != next.registryURL {
		t.Fatalf("active transport registry url = %q, want %q", status.RegistryURL, next.registryURL)
	}
}

func TestUpdateLibraryRelayConfigRestartsActiveTransport(t *testing.T) {
	ctx := context.Background()
	initial := openInviteTestInfra(t)
	next := openInviteTestInfra(t)
	app := openCacheTestApp(t, 1024)
	configureInviteTestApp(app, initial)

	var transports []*fakeManagedTransport
	app.transportService.setTransportFactoryForTest(func(_ context.Context, local apitypes.LocalContext) (managedSyncTransport, error) {
		transport := &fakeManagedTransport{
			libraryID: local.LibraryID,
			deviceID:  local.DeviceID,
			peerID:    fmt.Sprintf("test-peer-%d", len(transports)+1),
		}
		transports = append(transports, transport)
		return transport, nil
	})

	library, err := app.CreateLibrary(ctx, "relay-config-restart")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	if len(transports) != 1 {
		t.Fatalf("created transports = %d, want 1", len(transports))
	}
	if _, err := app.UpdateLibraryRelayConfig(ctx, apitypes.UpdateLibraryRelayConfigRequest{
		LibraryID:           library.LibraryID,
		RegistryURL:         next.registryURL,
		RelayBootstrapAddrs: next.relayBootstrapAddrs,
	}); err != nil {
		t.Fatalf("update relay config: %v", err)
	}
	if len(transports) != 2 {
		t.Fatalf("created transports after config update = %d, want 2", len(transports))
	}
	if transports[0].closed != 1 {
		t.Fatalf("original transport close count = %d, want 1", transports[0].closed)
	}
	status := app.NetworkStatus()
	if status.RegistryURL != next.registryURL {
		t.Fatalf("active registry url = %q, want %q", status.RegistryURL, next.registryURL)
	}
}

func TestInviteJoinRecoversAfterRelayReplacement(t *testing.T) {
	ctx := context.Background()
	infra := openInviteTestInfra(t)
	owner := openCacheTestApp(t, 1024)
	configureInviteTestApp(owner, infra)
	joiner := openCacheTestApp(t, 1024)
	configureInviteTestApp(joiner, infra)
	secondJoiner := openCacheTestApp(t, 1024)
	configureInviteTestApp(secondJoiner, infra)

	library, err := owner.CreateLibrary(ctx, "relay-replacement")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := owner.storage.WithContext(ctx).Create(&MembershipCertRevocation{
		LibraryID: library.LibraryID,
		DeviceID:  "removed-device",
		Serial:    1,
		PeerID:    "removed-peer",
		Reason:    "test relay replacement",
		RevokedAt: time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("seed membership revocation: %v", err)
	}
	pendingInvite, err := owner.CreateInvite(ctx, apitypes.InviteCreateRequest{})
	if err != nil {
		t.Fatalf("create pending invite: %v", err)
	}
	unusedInvite, err := owner.CreateInvite(ctx, apitypes.InviteCreateRequest{})
	if err != nil {
		t.Fatalf("create unused invite: %v", err)
	}
	attempt, err := joiner.StartJoinFromInvite(ctx, apitypes.JoinFromInviteInput{InviteCode: pendingInvite.InviteCode})
	if err != nil {
		t.Fatalf("start pending join: %v", err)
	}

	infra.replaceRelay(t)
	runtime := owner.transportService.activeRuntime()
	if runtime == nil {
		t.Fatal("owner transport runtime is not active")
	}
	owner.transportService.reconcileRuntimeRelay(runtime)
	if err := owner.transportService.announceRuntimePresence(runtime); err != nil {
		t.Fatalf("announce replacement relay presence: %v", err)
	}
	revocationSyncs := infra.registry.revocationSyncs()
	if len(revocationSyncs) != 1 || len(revocationSyncs[0].MembershipRevocations) != 1 || revocationSyncs[0].MembershipRevocations[0].DeviceID != "removed-device" {
		t.Fatalf("replacement relay revocation syncs = %+v", revocationSyncs)
	}

	requests, err := owner.ListJoinRequests(ctx)
	if err != nil || len(requests) != 1 {
		t.Fatalf("pending join requests = %+v, err=%v", requests, err)
	}
	if err := owner.ApproveJoinRequest(ctx, requests[0].RequestID); err != nil {
		t.Fatalf("approve pending join: %v", err)
	}
	completed, err := joiner.GetJoinAttempt(ctx, attempt.AttemptID)
	if err != nil {
		t.Fatalf("refresh pending join through replacement relay: %v", err)
	}
	if completed.Status != inviteJoinStatusCompleted {
		t.Fatalf("pending join status = %q, want completed", completed.Status)
	}

	second := startAndApproveJoin(t, ctx, owner, secondJoiner, unusedInvite.InviteCode)
	if second.Status != inviteJoinStatusCompleted {
		t.Fatalf("unused invite join status = %q, want completed", second.Status)
	}
}

func TestOpenInviteClientTransportReusesActiveSyncHost(t *testing.T) {
	ctx := context.Background()
	app := openPlaylistTestApp(t)

	library, err := app.CreateLibrary(ctx, "invite-reuse-host")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := app.syncActiveRuntimeServices(ctx); err != nil {
		t.Fatalf("start runtime services: %v", err)
	}
	runtime := app.transportService.activeRuntimeForLibrary(library.LibraryID)
	if runtime == nil || runtime.transport == nil {
		t.Fatal("expected active transport runtime")
	}
	transport, ok := runtime.transport.(*libp2pSyncTransport)
	if !ok || transport.host == nil {
		t.Fatal("expected active libp2p transport host")
	}

	client, err := app.openInviteClientTransport(nil)
	if err != nil {
		t.Fatalf("open invite client transport: %v", err)
	}
	if !client.sharedHost {
		t.Fatal("expected invite client transport to reuse the active sync host")
	}
	if client.host != transport.host {
		t.Fatal("expected invite client transport to reuse the active sync host instance")
	}

	if err := client.Close(); err != nil {
		t.Fatalf("close invite client transport: %v", err)
	}
	if runtime.ctx.Err() != nil {
		t.Fatalf("expected active transport runtime to remain active, err=%v", runtime.ctx.Err())
	}
	if transport.host == nil || transport.host.ID() == "" {
		t.Fatal("expected active sync host to remain open after invite client close")
	}
}

func TestFilterRelayInviteAddrsSkipsDirectAddrs(t *testing.T) {
	relayPeerID := mustGenerateTestPeerID(t)
	ownerPeerID := mustGenerateTestPeerID(t)
	addrs := filterRelayInviteAddrs([]string{
		fmt.Sprintf("/ip4/198.51.100.20/tcp/4001/p2p/%s", ownerPeerID),
		fmt.Sprintf("/ip4/198.51.100.21/tcp/4001/p2p/%s/p2p-circuit/p2p/%s", relayPeerID, ownerPeerID),
	})
	want := []string{fmt.Sprintf("/ip4/198.51.100.21/tcp/4001/p2p/%s/p2p-circuit/p2p/%s", relayPeerID, ownerPeerID)}
	if len(addrs) != len(want) || addrs[0] != want[0] {
		t.Fatalf("filter relay invite addrs = %#v, want %#v", addrs, want)
	}
}

func startAndApproveJoin(t *testing.T, ctx context.Context, owner, joiner *App, code string) apitypes.JoinAttempt {
	t.Helper()

	attempt, err := joiner.StartJoinFromInvite(ctx, apitypes.JoinFromInviteInput{InviteCode: code})
	if err != nil {
		t.Fatalf("start join: %v", err)
	}
	waitForPendingJoinRequest(t, ctx, owner, attempt.RequestID)
	if err := owner.ApproveJoinRequest(ctx, attempt.RequestID); err != nil {
		t.Fatalf("approve join request: %v", err)
	}
	return waitForJoinAttemptStatus(t, ctx, joiner, attempt.AttemptID, inviteJoinStatusCompleted)
}

func waitForPendingJoinRequest(t *testing.T, ctx context.Context, app *App, requestID string) apitypes.InviteJoinRequestRecord {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := app.ListJoinRequests(ctx)
		if err == nil {
			for _, row := range rows {
				if row.RequestID == requestID {
					return row
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	rows, err := app.ListJoinRequests(ctx)
	if err != nil {
		t.Fatalf("list join requests after wait: %v", err)
	}
	t.Fatalf("request %q not visible as pending: %+v", requestID, rows)
	return apitypes.InviteJoinRequestRecord{}
}

func waitForJoinAttemptStatus(t *testing.T, ctx context.Context, app *App, attemptID, want string) apitypes.JoinAttempt {
	t.Helper()

	deadline := time.Now().Add(20 * time.Second)
	var last apitypes.JoinAttempt
	var lastErr error
	for time.Now().Before(deadline) {
		attempt, err := app.GetJoinAttempt(ctx, attemptID)
		if err == nil {
			last = attempt
			if attempt.Status == want {
				return attempt
			}
		} else {
			lastErr = err
		}
		time.Sleep(50 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("join attempt %q did not reach %q; last error: %v; last attempt: %+v", attemptID, want, lastErr, last)
	}
	t.Fatalf("join attempt %q status = %q, want %q; attempt: %+v", attemptID, last.Status, want, last)
	return apitypes.JoinAttempt{}
}
