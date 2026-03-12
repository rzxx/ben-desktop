package desktopcore

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	apitypes "ben/core/api/types"
)

func TestScanRootsRequireActiveLibrary(t *testing.T) {
	t.Parallel()

	app := openCacheTestApp(t, 1024)
	_, err := app.ScanRoots(context.Background())
	if !errors.Is(err, apitypes.ErrNoActiveLibrary) {
		t.Fatalf("scan roots err = %v, want ErrNoActiveLibrary", err)
	}
}

func TestSetAndRemoveScanRootsNormalizeAndPersist(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	if _, err := app.CreateLibrary(ctx, "scan-roots"); err != nil {
		t.Fatalf("create library: %v", err)
	}

	rootBase := t.TempDir()
	rootA := filepath.Join(rootBase, "music-a")
	rootB := filepath.Join(rootBase, "music-b")

	if err := app.SetScanRoots(ctx, []string{
		rootA,
		filepath.Join(rootBase, ".", "music-a"),
		rootB,
		"",
	}); err != nil {
		t.Fatalf("set scan roots: %v", err)
	}

	got, err := app.ScanRoots(ctx)
	if err != nil {
		t.Fatalf("scan roots: %v", err)
	}
	want := []string{
		filepath.Clean(rootA),
		filepath.Clean(rootB),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scan roots = %v, want %v", got, want)
	}

	remaining, err := app.RemoveScanRoots(ctx, []string{filepath.Join(rootBase, ".", "music-b")})
	if err != nil {
		t.Fatalf("remove scan roots: %v", err)
	}
	if !reflect.DeepEqual(remaining, []string{filepath.Clean(rootA)}) {
		t.Fatalf("remaining roots = %v, want [%s]", remaining, filepath.Clean(rootA))
	}
}

func TestAddScanRootsReturnsMergedNormalizedRoots(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	if _, err := app.CreateLibrary(ctx, "scan-roots-add"); err != nil {
		t.Fatalf("create library: %v", err)
	}

	rootBase := t.TempDir()
	rootA := filepath.Join(rootBase, "music-a")
	rootB := filepath.Join(rootBase, "music-b")
	if err := app.SetScanRoots(ctx, []string{rootA}); err != nil {
		t.Fatalf("seed scan roots: %v", err)
	}

	got, err := app.AddScanRoots(ctx, []string{filepath.Join(rootBase, ".", "music-a"), rootB})
	if err != nil {
		t.Fatalf("add scan roots: %v", err)
	}
	want := []string{
		filepath.Clean(rootA),
		filepath.Clean(rootB),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("added roots = %v, want %v", got, want)
	}
}

func TestScanRootUpdatesRejectGuestRole(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openCacheTestApp(t, 1024)
	library, err := app.CreateLibrary(ctx, "scan-roots-guest")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.requireActiveContext(ctx)
	if err != nil {
		t.Fatalf("active context: %v", err)
	}
	if err := app.db.WithContext(ctx).
		Model(&Membership{}).
		Where("library_id = ? AND device_id = ?", library.LibraryID, local.DeviceID).
		Update("role", roleGuest).Error; err != nil {
		t.Fatalf("set guest role: %v", err)
	}

	root := filepath.Join(t.TempDir(), "guest-root")
	if err := app.SetScanRoots(ctx, []string{root}); err == nil || err.Error() != "scan root updates require owner, admin, or member role" {
		t.Fatalf("set scan roots err = %v", err)
	}
	if _, err := app.AddScanRoots(ctx, []string{root}); err == nil || err.Error() != "scan root updates require owner, admin, or member role" {
		t.Fatalf("add scan roots err = %v", err)
	}
	if _, err := app.RemoveScanRoots(ctx, []string{root}); err == nil || err.Error() != "scan root updates require owner, admin, or member role" {
		t.Fatalf("remove scan roots err = %v", err)
	}
}
