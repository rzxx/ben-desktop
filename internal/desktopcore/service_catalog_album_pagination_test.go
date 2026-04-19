package desktopcore

import (
	"context"
	"reflect"
	"testing"
	"time"

	apitypes "ben/desktop/api/types"
)

func TestCatalogAlbumPaginationCollapsesBeforePaging(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := openPlaylistTestApp(t)
	library, err := app.CreateLibrary(ctx, "catalog-album-pagination")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}

	seedAlbumVariantForList(t, app, library.LibraryID, "album-b-variant-1", "cluster-b", "Bravo")
	seedAlbumVariantForList(t, app, library.LibraryID, "album-c-variant-1", "cluster-c", "alpha")
	seedAlbumVariantForList(t, app, library.LibraryID, "album-a-variant-1", "cluster-a", "Alpha")
	seedAlbumVariantForList(t, app, library.LibraryID, "album-a-variant-2", "cluster-a", "Alpha Deluxe")

	order := collectAlbumOrderByOffset(t, ctx, app, 1)
	want := []string{"cluster-a", "cluster-c", "cluster-b"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("offset album order = %v, want %v", order, want)
	}
}

func seedAlbumVariantForList(t *testing.T, app *App, libraryID, albumVariantID, albumClusterID, title string) {
	t.Helper()

	now := time.Now().UTC()
	if err := app.db.WithContext(context.Background()).Create(&AlbumVariantModel{
		LibraryID:      libraryID,
		AlbumVariantID: albumVariantID,
		AlbumClusterID: albumClusterID,
		KeyNorm:        title,
		Title:          title,
		CreatedAt:      now,
		UpdatedAt:      now,
	}).Error; err != nil {
		t.Fatalf("seed album variant %q: %v", albumVariantID, err)
	}
}

func collectAlbumOrderByOffset(t *testing.T, ctx context.Context, app *App, limit int) []string {
	t.Helper()

	order := make([]string, 0)
	for offset := 0; ; {
		page, err := app.ListAlbums(ctx, apitypes.AlbumListRequest{
			PageRequest: apitypes.PageRequest{
				Limit:  limit,
				Offset: offset,
			},
		})
		if err != nil {
			t.Fatalf("list albums offset page: %v", err)
		}
		for _, item := range page.Items {
			order = append(order, item.AlbumID)
		}
		if !page.Page.HasMore {
			return order
		}
		offset = page.Page.NextOffset
	}
}
