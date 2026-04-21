package desktopcore

import (
	"ben/registryauth"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	localSettingPeerAddrsPrefix = "peer_addrs:"
	registryRequestTimeout      = 10 * time.Second
)

type PeerLocator interface {
	Announce(ctx context.Context, req registryauth.PresenceAnnounceRequest) error
	LookupMemberPeer(ctx context.Context, req registryauth.MemberLookupRequest) (PeerPresenceRecord, bool, error)
	LookupInviteOwner(ctx context.Context, req registryauth.InviteOwnerLookupRequest) (PeerPresenceRecord, bool, error)
}

type PeerPresenceRecord = registryauth.PresenceRecord

type httpPeerLocator struct {
	baseURL string
	client  *http.Client
}

func newPeerLocator(baseURL string) PeerLocator {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return nil
	}
	return &httpPeerLocator{
		baseURL: strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Timeout: registryRequestTimeout,
		},
	}
}

func (l *httpPeerLocator) Announce(ctx context.Context, req registryauth.PresenceAnnounceRequest) error {
	if l == nil {
		return fmt.Errorf("peer locator is not configured")
	}
	req.Record.LibraryID = strings.TrimSpace(req.Record.LibraryID)
	req.Record.DeviceID = strings.TrimSpace(req.Record.DeviceID)
	req.Record.PeerID = strings.TrimSpace(req.Record.PeerID)
	req.Record.Addrs = compactNonEmptyStrings(req.Record.Addrs)
	req.RootPublicKey = strings.TrimSpace(req.RootPublicKey)
	if req.Record.LibraryID == "" || req.Record.DeviceID == "" || req.Record.PeerID == "" {
		return fmt.Errorf("library id, device id, and peer id are required")
	}
	if req.RootPublicKey == "" {
		return fmt.Errorf("root public key is required")
	}
	if req.Record.UpdatedAt.IsZero() {
		req.Record.UpdatedAt = time.Now().UTC()
	}
	if req.Record.ExpiresAt.IsZero() {
		req.Record.ExpiresAt = req.Record.UpdatedAt.Add(90 * time.Second)
	}
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal registry announce: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, l.baseURL+"/v1/presence/announce", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build registry announce request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := l.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("announce presence: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("announce presence: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

func (l *httpPeerLocator) LookupMemberPeer(ctx context.Context, req registryauth.MemberLookupRequest) (PeerPresenceRecord, bool, error) {
	if l == nil {
		return PeerPresenceRecord{}, false, fmt.Errorf("peer locator is not configured")
	}
	req.LibraryID = strings.TrimSpace(req.LibraryID)
	req.PeerID = strings.TrimSpace(req.PeerID)
	req.RootPublicKey = strings.TrimSpace(req.RootPublicKey)
	return l.lookup(ctx, "/v1/presence/member", req)
}

func (l *httpPeerLocator) LookupInviteOwner(ctx context.Context, req registryauth.InviteOwnerLookupRequest) (PeerPresenceRecord, bool, error) {
	if l == nil {
		return PeerPresenceRecord{}, false, fmt.Errorf("peer locator is not configured")
	}
	return l.lookup(ctx, "/v1/invites/owner", req)
}

func (l *httpPeerLocator) lookup(ctx context.Context, path string, payload any) (PeerPresenceRecord, bool, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return PeerPresenceRecord{}, false, fmt.Errorf("marshal registry lookup request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, l.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return PeerPresenceRecord{}, false, fmt.Errorf("build registry lookup request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := l.client.Do(httpReq)
	if err != nil {
		return PeerPresenceRecord{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return PeerPresenceRecord{}, false, nil
	}
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return PeerPresenceRecord{}, false, fmt.Errorf("registry lookup failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	var record PeerPresenceRecord
	if err := json.NewDecoder(resp.Body).Decode(&record); err != nil {
		return PeerPresenceRecord{}, false, fmt.Errorf("decode registry response: %w", err)
	}
	record.LibraryID = strings.TrimSpace(record.LibraryID)
	record.DeviceID = strings.TrimSpace(record.DeviceID)
	record.PeerID = strings.TrimSpace(record.PeerID)
	record.Addrs = compactNonEmptyStrings(record.Addrs)
	if record.PeerID == "" {
		return PeerPresenceRecord{}, false, nil
	}
	return record, true, nil
}

func peerAddrsLocalSettingKey(libraryID, peerID string) string {
	libraryID = strings.TrimSpace(libraryID)
	peerID = strings.TrimSpace(peerID)
	if libraryID == "" || peerID == "" {
		return ""
	}
	return localSettingPeerAddrsPrefix + libraryID + ":" + peerID
}

func (a *App) saveKnownPeerAddrs(ctx context.Context, libraryID, peerID string, addrs []string) error {
	key := peerAddrsLocalSettingKey(libraryID, peerID)
	if key == "" {
		return nil
	}
	addrs = compactNonEmptyStrings(addrs)
	if len(addrs) == 0 {
		return nil
	}
	body, err := json.Marshal(addrs)
	if err != nil {
		return fmt.Errorf("encode peer addrs cache: %w", err)
	}
	return upsertLocalSettingTx(a.storage.WithContext(ctx), key, string(body), time.Now().UTC())
}

func (a *App) loadKnownPeerAddrs(ctx context.Context, libraryID, peerID string) ([]string, error) {
	key := peerAddrsLocalSettingKey(libraryID, peerID)
	if key == "" {
		return nil, nil
	}
	var row LocalSetting
	if err := a.storage.WithContext(ctx).Where("key = ?", key).Take(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	var addrs []string
	if err := json.Unmarshal([]byte(strings.TrimSpace(row.Value)), &addrs); err != nil {
		return nil, fmt.Errorf("decode peer addrs cache: %w", err)
	}
	return compactNonEmptyStrings(addrs), nil
}
