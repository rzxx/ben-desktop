package desktopcore

import (
	"context"
	"fmt"
	"path/filepath"
	stdruntime "runtime"
	"strings"
	"time"

	"gorm.io/gorm"
)

type IngestService struct {
	app *App
}

func (s *IngestService) SetScanRoots(ctx context.Context, roots []string) error {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return err
	}
	if !canProvideLocalMedia(local.Role) {
		return fmt.Errorf("scan root updates require owner, admin, or member role")
	}
	current, err := s.app.scanRootsForDevice(ctx, local.LibraryID, local.DeviceID)
	if err != nil {
		return err
	}
	normalized, err := normalizeScanRoots(roots)
	if err != nil {
		return err
	}
	if sameScanRootSet(current, normalized) {
		return nil
	}
	removed := diffScanRoots(current, normalized)
	if err := s.app.storage.Transaction(ctx, func(tx *gorm.DB) error {
		return setLibraryScanRootsTx(tx, local.LibraryID, local.DeviceID, normalized)
	}); err != nil {
		return err
	}
	if s.app.scanner != nil {
		s.app.scanner.resetRuntime(local.LibraryID, local.DeviceID)
	}
	if err := s.removeScanRootsFromCatalog(ctx, local.LibraryID, local.DeviceID, removed); err != nil {
		return s.restoreScanRootsAfterFailure(ctx, local.LibraryID, local.DeviceID, current, err)
	}
	if err := s.app.syncActiveScanWatcher(ctx); err != nil {
		return s.restoreScanRootsAfterFailure(ctx, local.LibraryID, local.DeviceID, current, err)
	}
	return nil
}

func (s *IngestService) AddScanRoots(ctx context.Context, roots []string) ([]string, error) {
	current, err := s.ScanRoots(ctx)
	if err != nil {
		return nil, err
	}
	next := append(append([]string(nil), current...), roots...)
	if err := s.SetScanRoots(ctx, next); err != nil {
		return nil, err
	}
	return s.ScanRoots(ctx)
}

func (s *IngestService) RemoveScanRoots(ctx context.Context, roots []string) ([]string, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return nil, err
	}
	if !canProvideLocalMedia(local.Role) {
		return nil, fmt.Errorf("scan root updates require owner, admin, or member role")
	}
	current, err := s.app.scanRootsForDevice(ctx, local.LibraryID, local.DeviceID)
	if err != nil {
		return nil, err
	}
	targets, err := normalizeScanRoots(roots)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return current, nil
	}

	next := make([]string, 0, len(current))
	for _, root := range current {
		if hasScanRoot(targets, root) {
			continue
		}
		next = append(next, root)
	}

	if err := s.SetScanRoots(ctx, next); err != nil {
		return nil, err
	}
	return s.app.scanRootsForDevice(ctx, local.LibraryID, local.DeviceID)
}

func (s *IngestService) ScanRoots(ctx context.Context) ([]string, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return nil, err
	}
	return s.app.scanRootsForDevice(ctx, local.LibraryID, local.DeviceID)
}

func (s *IngestService) restoreScanRootsAfterFailure(
	ctx context.Context,
	libraryID string,
	deviceID string,
	previous []string,
	cause error,
) error {
	if s == nil || s.app == nil {
		return cause
	}

	restoreErr := s.app.storage.Transaction(ctx, func(tx *gorm.DB) error {
		return setLibraryScanRootsTx(tx, libraryID, deviceID, previous)
	})
	if restoreErr != nil {
		return fmt.Errorf("%w (also failed to restore previous scan roots: %v)", cause, restoreErr)
	}

	if len(previous) > 0 {
		s.app.markStartupScanPending(libraryID, deviceID)
	}
	if syncErr := s.app.syncActiveScanWatcher(ctx); syncErr != nil {
		return fmt.Errorf("%w (previous scan roots were restored, but watcher recovery failed: %v)", cause, syncErr)
	}
	return cause
}

func (a *App) scanRootsForDevice(ctx context.Context, libraryID, deviceID string) ([]string, error) {
	var rows []ScanRoot
	if err := a.storage.WithContext(ctx).
		Where("library_id = ? AND device_id = ?", libraryID, deviceID).
		Order("root_path ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		root := strings.TrimSpace(row.RootPath)
		if root == "" {
			continue
		}
		out = append(out, root)
	}
	return out, nil
}

func setLibraryScanRootsTx(tx *gorm.DB, libraryID, deviceID string, roots []string) error {
	now := time.Now().UTC()
	clean := make([]ScanRoot, 0, len(roots))
	seen := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		if _, ok := seen[root]; ok {
			continue
		}
		seen[root] = struct{}{}
		clean = append(clean, ScanRoot{
			LibraryID: libraryID,
			DeviceID:  deviceID,
			RootPath:  root,
			CreatedAt: now,
			UpdatedAt: now,
		})
	}
	if err := tx.Where("library_id = ? AND device_id = ?", libraryID, deviceID).Delete(&ScanRoot{}).Error; err != nil {
		return err
	}
	if len(clean) == 0 {
		return nil
	}
	return tx.Create(&clean).Error
}

func normalizeScanRoots(roots []string) ([]string, error) {
	if len(roots) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(roots))
	seen := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		resolved, err := filepath.Abs(root)
		if err != nil {
			return nil, fmt.Errorf("resolve scan root %q: %w", root, err)
		}
		resolved = filepath.Clean(resolved)
		key := scanRootKey(resolved)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, resolved)
	}
	return out, nil
}

func hasScanRoot(targets []string, candidate string) bool {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return false
	}
	resolved, err := filepath.Abs(candidate)
	if err != nil {
		return false
	}
	resolved = filepath.Clean(resolved)
	candidateKey := scanRootKey(resolved)
	for _, target := range targets {
		if scanRootKey(target) == candidateKey {
			return true
		}
	}
	return false
}

func diffScanRoots(current, next []string) []string {
	if len(current) == 0 {
		return nil
	}
	nextSet := make(map[string]struct{}, len(next))
	for _, root := range normalizedWatcherRoots(next) {
		nextSet[scanRootKey(root)] = struct{}{}
	}
	removed := make([]string, 0, len(current))
	for _, root := range normalizedWatcherRoots(current) {
		if _, ok := nextSet[scanRootKey(root)]; ok {
			continue
		}
		removed = append(removed, root)
	}
	return removed
}

func scanRootKey(root string) string {
	root = filepath.Clean(strings.TrimSpace(root))
	if stdruntime.GOOS == "windows" {
		return strings.ToLower(root)
	}
	return root
}
