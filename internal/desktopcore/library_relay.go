package desktopcore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	apitypes "ben/desktop/api/types"

	"gorm.io/gorm"
)

type resolvedRelayConfig struct {
	LibraryID           string
	RegistryURL         string
	RelayBootstrapAddrs []string
}

func (a *App) relayConfigForLibrary(ctx context.Context, libraryID string) (resolvedRelayConfig, error) {
	cfg := resolvedRelayConfig{
		LibraryID: strings.TrimSpace(libraryID),
	}
	if a != nil {
		cfg.RegistryURL = strings.TrimSpace(a.cfg.RegistryURL)
		cfg.RelayBootstrapAddrs = compactNonEmptyStrings(a.cfg.RelayBootstrapAddrs)
	}
	if a == nil || a.storage == nil || cfg.LibraryID == "" {
		return cfg, nil
	}

	var library Library
	err := a.storage.WithContext(ctx).
		Select("library_id", "registry_url", "relay_bootstrap_addrs_json").
		Where("library_id = ?", cfg.LibraryID).
		Take(&library).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return cfg, nil
		}
		return resolvedRelayConfig{}, err
	}
	if registryURL := strings.TrimSpace(library.RegistryURL); registryURL != "" {
		cfg.RegistryURL = registryURL
	}
	if addrs := decodeStringListJSON(library.RelayBootstrapAddrsJSON); len(addrs) > 0 {
		cfg.RelayBootstrapAddrs = addrs
	}
	return cfg, nil
}

func (a *App) peerLocatorForLibrary(ctx context.Context, libraryID string) (PeerLocator, string, error) {
	cfg, err := a.relayConfigForLibrary(ctx, libraryID)
	if err != nil {
		return nil, "", err
	}
	return newPeerLocator(cfg.RegistryURL), cfg.RegistryURL, nil
}

func (a *App) peerLocator(registryURL string) PeerLocator {
	if a != nil && strings.TrimSpace(a.cfg.RegistryURL) != "" {
		registryURL = a.cfg.RegistryURL
	}
	return newPeerLocator(registryURL)
}

func (a *App) relayBootstrapAddrsForLibrary(ctx context.Context, libraryID string, extra []string) ([]string, error) {
	cfg, err := a.relayConfigForLibrary(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(cfg.RelayBootstrapAddrs)+len(extra))
	out = append(out, a.discoverRelayBootstrapAddrsFromRegistryURL(cfg.RegistryURL)...)
	out = append(out, cfg.RelayBootstrapAddrs...)
	out = append(out, extra...)
	return compactNonEmptyStrings(out), nil
}

func relayConfigRecord(libraryID string, cfg resolvedRelayConfig) apitypes.LibraryRelayConfig {
	return apitypes.LibraryRelayConfig{
		LibraryID:           strings.TrimSpace(libraryID),
		RegistryURL:         strings.TrimSpace(cfg.RegistryURL),
		RelayBootstrapAddrs: compactNonEmptyStrings(cfg.RelayBootstrapAddrs),
	}
}

func encodeStringListJSON(items []string) string {
	items = compactNonEmptyStrings(items)
	if len(items) == 0 {
		return ""
	}
	body, err := json.Marshal(items)
	if err != nil {
		return ""
	}
	return string(body)
}

func decodeStringListJSON(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var items []string
	if err := json.Unmarshal([]byte(value), &items); err != nil {
		return nil
	}
	return compactNonEmptyStrings(items)
}

func (s *LibraryService) GetLibraryRelayConfig(ctx context.Context, libraryID string) (apitypes.LibraryRelayConfig, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.LibraryRelayConfig{}, err
	}
	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" {
		libraryID = local.LibraryID
	}
	if libraryID != local.LibraryID {
		return apitypes.LibraryRelayConfig{}, fmt.Errorf("library relay config requires the active library")
	}
	cfg, err := s.app.relayConfigForLibrary(ctx, libraryID)
	if err != nil {
		return apitypes.LibraryRelayConfig{}, err
	}
	return relayConfigRecord(libraryID, cfg), nil
}

func (s *LibraryService) UpdateLibraryRelayConfig(ctx context.Context, req apitypes.UpdateLibraryRelayConfigRequest) (apitypes.LibraryRelayConfig, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.LibraryRelayConfig{}, err
	}
	if !canManageLibrary(local.Role) {
		return apitypes.LibraryRelayConfig{}, fmt.Errorf("library relay config requires owner or admin role")
	}
	libraryID := strings.TrimSpace(req.LibraryID)
	if libraryID == "" {
		libraryID = local.LibraryID
	}
	if libraryID != local.LibraryID {
		return apitypes.LibraryRelayConfig{}, fmt.Errorf("library relay config requires the active library")
	}
	registryURL := strings.TrimSpace(req.RegistryURL)
	relayBootstrap := compactNonEmptyStrings(req.RelayBootstrapAddrs)
	if err := s.app.storage.WithContext(ctx).
		Model(&Library{}).
		Where("library_id = ?", libraryID).
		Updates(map[string]any{
			"registry_url":               registryURL,
			"relay_bootstrap_addrs_json": encodeStringListJSON(relayBootstrap),
		}).Error; err != nil {
		return apitypes.LibraryRelayConfig{}, err
	}
	if s.app.invite != nil {
		s.app.invite.clearLibraryRuntimeState(libraryID)
	}
	if err := s.app.syncActiveRuntimeServices(ctx); err != nil {
		return apitypes.LibraryRelayConfig{}, err
	}
	cfg, err := s.app.relayConfigForLibrary(ctx, libraryID)
	if err != nil {
		return apitypes.LibraryRelayConfig{}, err
	}
	return relayConfigRecord(libraryID, cfg), nil
}
