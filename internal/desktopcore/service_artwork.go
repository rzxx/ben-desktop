package desktopcore

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	apitypes "ben/core/api/types"
	"gorm.io/gorm"
)

var ErrNoArtworkFound = errors.New("no cover artwork found")

type ArtworkVariantSpec struct {
	Name   string
	Size   int
	Format string
	MIME   string
}

type GeneratedArtworkVariant struct {
	Variant string
	MIME    string
	FileExt string
	Bytes   []byte
	W       int
	H       int
}

type ArtworkBuildResult struct {
	SourceKind string
	SourceRef  string
	Variants   []GeneratedArtworkVariant
}

type ArtworkBuilder interface {
	BuildForAudio(ctx context.Context, audioPath string) (ArtworkBuildResult, error)
}

type ffmpegArtworkBuilder struct {
	ffmpegPath string
	variants   []ArtworkVariantSpec
}

type ArtworkService struct {
	app     *App
	builder ArtworkBuilder
}

func newArtworkService(app *App) *ArtworkService {
	if app == nil {
		return nil
	}
	builder := app.cfg.ArtworkBuilder
	if builder == nil {
		builder = NewArtworkBuilder(strings.TrimSpace(app.cfg.FFmpegPath))
	}
	return &ArtworkService{app: app, builder: builder}
}

func NewArtworkBuilder(ffmpegPath string) ArtworkBuilder {
	if strings.TrimSpace(ffmpegPath) == "" {
		ffmpegPath = "ffmpeg"
	}
	return &ffmpegArtworkBuilder{
		ffmpegPath: ffmpegPath,
		variants: []ArtworkVariantSpec{
			{Name: defaultArtworkVariant96, Size: 96, Format: "jpg", MIME: "image/jpeg"},
			{Name: defaultArtworkVariant320, Size: 320, Format: "webp", MIME: "image/webp"},
			{Name: defaultArtworkVariant1024, Size: 1024, Format: "avif", MIME: "image/avif"},
		},
	}
}

func (b *ffmpegArtworkBuilder) BuildForAudio(ctx context.Context, audioPath string) (ArtworkBuildResult, error) {
	sourceKind, sourceRef, sourceImage, cleanup, err := b.selectSource(ctx, audioPath)
	if err != nil {
		return ArtworkBuildResult{}, err
	}
	defer cleanup()

	variants := make([]GeneratedArtworkVariant, 0, len(b.variants))
	for _, spec := range b.variants {
		data, err := b.renderVariant(ctx, sourceImage, spec)
		if err != nil {
			return ArtworkBuildResult{}, err
		}
		variants = append(variants, GeneratedArtworkVariant{
			Variant: spec.Name,
			MIME:    spec.MIME,
			FileExt: imageExtensionForMIME(spec.MIME),
			Bytes:   data,
			W:       spec.Size,
			H:       spec.Size,
		})
	}

	return ArtworkBuildResult{
		SourceKind: sourceKind,
		SourceRef:  sourceRef,
		Variants:   variants,
	}, nil
}

func (b *ffmpegArtworkBuilder) selectSource(ctx context.Context, audioPath string) (kind string, ref string, generatedPath string, cleanup func(), err error) {
	tempDir, err := os.MkdirTemp("", "ben-cover-*")
	if err != nil {
		return "", "", "", func() {}, fmt.Errorf("create artwork temp dir: %w", err)
	}

	embeddedOut := filepath.Join(tempDir, "embedded.jpg")
	embeddedArgs := []string{"-hide_banner", "-loglevel", "error", "-y", "-i", audioPath, "-an", "-map", "0:v:0", "-frames:v", "1", embeddedOut}
	if err := runFFmpeg(ctx, b.ffmpegPath, embeddedArgs); err == nil {
		if st, statErr := os.Stat(embeddedOut); statErr == nil && st.Size() > 0 {
			return "embedded", audioPath, embeddedOut, func() { _ = os.RemoveAll(tempDir) }, nil
		}
	}
	_ = os.RemoveAll(tempDir)

	sidecar := findSidecarImage(audioPath)
	if sidecar != "" {
		return "sidecar", sidecar, sidecar, func() {}, nil
	}

	return "", "", "", func() {}, fmt.Errorf("%w for %s", ErrNoArtworkFound, audioPath)
}

func (b *ffmpegArtworkBuilder) renderVariant(ctx context.Context, input string, spec ArtworkVariantSpec) ([]byte, error) {
	tempDir, err := os.MkdirTemp("", "ben-thumb-*")
	if err != nil {
		return nil, fmt.Errorf("create variant temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	out := filepath.Join(tempDir, fmt.Sprintf("thumb-%s.%s", spec.Name, spec.Format))
	filter := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=increase:flags=lanczos,crop=%d:%d", spec.Size, spec.Size, spec.Size, spec.Size)
	args := []string{"-hide_banner", "-loglevel", "error", "-y", "-i", input, "-vf", filter}
	args = append(args, artworkVariantCodecArgs(spec)...)
	args = append(args, "-frames:v", "1", out)
	if err := runFFmpeg(ctx, b.ffmpegPath, args); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(out)
	if err != nil {
		return nil, fmt.Errorf("read generated artwork: %w", err)
	}
	return data, nil
}

func artworkVariantCodecArgs(spec ArtworkVariantSpec) []string {
	switch spec.Format {
	case "jpg":
		return []string{"-q:v", "2"}
	case "webp":
		return []string{"-c:v", "libwebp", "-quality", "85", "-compression_level", "6"}
	case "avif":
		return []string{"-c:v", "libaom-av1", "-still-picture", "1", "-crf", "30", "-cpu-used", "4", "-pix_fmt", "yuv420p"}
	default:
		return nil
	}
}

func imageExtensionForMIME(imageMIME string) string {
	imageMIME = strings.TrimSpace(strings.ToLower(imageMIME))
	if imageMIME == "" {
		return ".img"
	}
	extensions, _ := mime.ExtensionsByType(imageMIME)
	for _, ext := range extensions {
		if normalized := normalizeArtworkFileExt(ext, imageMIME); normalized != "" {
			return normalized
		}
	}
	switch imageMIME {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/avif":
		return ".avif"
	case "image/gif":
		return ".gif"
	default:
		return ".img"
	}
}

func findSidecarImage(audioPath string) string {
	dir := filepath.Dir(audioPath)
	parent := filepath.Dir(dir)
	for _, candidate := range sidecarCandidates(dir) {
		if sidecarIsFile(candidate) {
			return candidate
		}
	}
	for _, candidate := range sidecarCandidates(parent) {
		if sidecarIsFile(candidate) {
			return candidate
		}
	}
	return ""
}

func sidecarCandidates(dir string) []string {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	names := []string{
		"cover.jpg", "cover.jpeg", "cover.png", "cover.webp",
		"folder.jpg", "folder.jpeg", "folder.png", "folder.webp",
		"front.jpg", "front.jpeg", "front.png", "front.webp",
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, filepath.Join(dir, name))
	}
	return out
}

func sidecarIsFile(path string) bool {
	st, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !st.IsDir()
}

func (s *ArtworkService) ReconcileAlbumArtwork(ctx context.Context, local apitypes.LocalContext, albumID string) error {
	if s == nil || s.app == nil || s.builder == nil {
		return nil
	}
	albumID = strings.TrimSpace(albumID)
	if albumID == "" {
		return nil
	}

	s.app.setArtworkActivity(apitypes.ArtworkActivityStatus{
		Phase:          "running",
		AlbumsTotal:    1,
		CurrentAlbumID: albumID,
		Workers:        1,
		WorkersActive:  1,
	})

	err := s.reconcileAlbumArtwork(ctx, local, albumID)
	if err != nil {
		s.app.setArtworkActivity(apitypes.ArtworkActivityStatus{
			Phase:          "failed",
			AlbumsTotal:    1,
			AlbumsDone:     1,
			CurrentAlbumID: albumID,
			Workers:        1,
			Errors:         1,
		})
		return err
	}

	s.app.setArtworkActivity(apitypes.ArtworkActivityStatus{
		Phase:       "completed",
		AlbumsTotal: 1,
		AlbumsDone:  1,
		Workers:     1,
	})
	return nil
}

func (s *ArtworkService) reconcileAlbumArtwork(ctx context.Context, local apitypes.LocalContext, albumID string) error {
	existing, err := s.loadScopeArtwork(ctx, local.LibraryID, "album", albumID)
	if err != nil {
		return err
	}
	chosenSourceRef := ""
	if len(existing) > 0 {
		_, chosenSourceRef, _, err = localArtworkSourceRefForScopeTx(s.app.db.WithContext(ctx), local.LibraryID, "album", albumID, existing[0].Variant)
		if err != nil {
			return err
		}
	}

	candidates, err := s.listAlbumArtworkSources(ctx, local.LibraryID, local.DeviceID, albumID)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		if len(existing) == 0 {
			return nil
		}
		ownsCurrent, err := s.localDeviceOwnsArtworkSource(ctx, local.LibraryID, local.DeviceID, chosenSourceRef)
		if err != nil {
			return err
		}
		if ownsCurrent {
			return s.deleteAlbumArtwork(ctx, local, albumID)
		}
		return nil
	}

	if len(existing) > 0 && artworkVariantsComplete(existing) {
		if _, err := os.Stat(chosenSourceRef); err == nil {
			return nil
		}
	}

	built, err := s.buildAlbumArtwork(ctx, candidates)
	if err != nil {
		if errors.Is(err, ErrNoArtworkFound) {
			if len(existing) == 0 {
				return nil
			}
			ownsCurrent, ownErr := s.localDeviceOwnsArtworkSource(ctx, local.LibraryID, local.DeviceID, chosenSourceRef)
			if ownErr != nil {
				return ownErr
			}
			if ownsCurrent {
				return s.deleteAlbumArtwork(ctx, local, albumID)
			}
			return nil
		}
		return err
	}
	return s.storeArtworkScope(ctx, local, "album", albumID, built)
}

func (s *ArtworkService) buildAlbumArtwork(ctx context.Context, candidates []SourceFileModel) (ArtworkBuildResult, error) {
	for _, candidate := range candidates {
		path := filepath.Clean(strings.TrimSpace(candidate.LocalPath))
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		built, err := s.builder.BuildForAudio(ctx, path)
		if err == nil {
			return built, nil
		}
		if errors.Is(err, ErrNoArtworkFound) {
			continue
		}
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			continue
		}
		return ArtworkBuildResult{}, err
	}
	return ArtworkBuildResult{}, ErrNoArtworkFound
}

func (s *ArtworkService) storeArtworkScope(ctx context.Context, local apitypes.LocalContext, scopeType, scopeID string, built ArtworkBuildResult) error {
	if len(built.Variants) == 0 {
		return ErrNoArtworkFound
	}
	now := time.Now().UTC()
	return s.app.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, variant := range built.Variants {
			blobID, err := s.app.transcode.storeBlobBytes(variant.Bytes)
			if err != nil {
				return err
			}
			if err := s.app.upsertArtworkVariantTx(tx, local, ArtworkVariant{
				LibraryID:       local.LibraryID,
				ScopeType:       strings.TrimSpace(scopeType),
				ScopeID:         strings.TrimSpace(scopeID),
				Variant:         strings.TrimSpace(variant.Variant),
				BlobID:          blobID,
				MIME:            strings.TrimSpace(variant.MIME),
				FileExt:         normalizeArtworkFileExt(variant.FileExt, variant.MIME),
				W:               variant.W,
				H:               variant.H,
				Bytes:           int64(len(variant.Bytes)),
				ChosenSource:    strings.TrimSpace(built.SourceKind),
				ChosenSourceRef: strings.TrimSpace(built.SourceRef),
				UpdatedAt:       now,
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *ArtworkService) deleteAlbumArtwork(ctx context.Context, local apitypes.LocalContext, albumID string) error {
	return s.app.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return s.app.deleteArtworkScopeTx(tx, local, "album", albumID)
	})
}

func (s *ArtworkService) loadScopeArtwork(ctx context.Context, libraryID, scopeType, scopeID string) ([]ArtworkVariant, error) {
	var rows []ArtworkVariant
	if err := s.app.db.WithContext(ctx).
		Where("library_id = ? AND scope_type = ? AND scope_id = ?", strings.TrimSpace(libraryID), strings.TrimSpace(scopeType), strings.TrimSpace(scopeID)).
		Order("variant ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *ArtworkService) listAlbumArtworkSources(ctx context.Context, libraryID, deviceID, albumID string) ([]SourceFileModel, error) {
	var rows []SourceFileModel
	query := `
SELECT DISTINCT sf.*
FROM source_files sf
JOIN album_tracks at ON at.library_id = sf.library_id AND at.track_variant_id = sf.track_variant_id
WHERE sf.library_id = ? AND sf.device_id = ? AND at.album_variant_id = ? AND sf.is_present = 1
ORDER BY sf.quality_rank DESC, sf.last_seen_at DESC, sf.size_bytes DESC, sf.local_path ASC`
	if err := s.app.db.WithContext(ctx).Raw(query, strings.TrimSpace(libraryID), strings.TrimSpace(deviceID), strings.TrimSpace(albumID)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *ArtworkService) albumIDsForTrackVariants(ctx context.Context, libraryID string, trackVariantIDs []string) ([]string, error) {
	clean := make([]string, 0, len(trackVariantIDs))
	seen := make(map[string]struct{}, len(trackVariantIDs))
	for _, trackVariantID := range trackVariantIDs {
		trackVariantID = strings.TrimSpace(trackVariantID)
		if trackVariantID == "" {
			continue
		}
		if _, ok := seen[trackVariantID]; ok {
			continue
		}
		seen[trackVariantID] = struct{}{}
		clean = append(clean, trackVariantID)
	}
	if len(clean) == 0 {
		return nil, nil
	}

	type row struct {
		AlbumVariantID string
	}
	var rows []row
	if err := s.app.db.WithContext(ctx).
		Table("album_tracks").
		Select("DISTINCT album_variant_id AS album_variant_id").
		Where("library_id = ? AND track_variant_id IN ?", strings.TrimSpace(libraryID), clean).
		Order("album_variant_id ASC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	out := make([]string, 0, len(rows))
	for _, row := range rows {
		albumID := strings.TrimSpace(row.AlbumVariantID)
		if albumID != "" {
			out = append(out, albumID)
		}
	}
	return out, nil
}

func (s *ArtworkService) localDeviceOwnsArtworkSource(ctx context.Context, libraryID, deviceID, sourceRef string) (bool, error) {
	sourceRef = filepath.Clean(strings.TrimSpace(sourceRef))
	if sourceRef == "" {
		return false, nil
	}
	var count int64
	if err := s.app.db.WithContext(ctx).
		Model(&LocalSourcePath{}).
		Where("library_id = ? AND device_id = ? AND path_key = ?", strings.TrimSpace(libraryID), strings.TrimSpace(deviceID), localPathKey(sourceRef)).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func artworkVariantsComplete(rows []ArtworkVariant) bool {
	if len(rows) == 0 {
		return false
	}
	required := map[string]bool{
		defaultArtworkVariant96:   false,
		defaultArtworkVariant320:  false,
		defaultArtworkVariant1024: false,
	}
	for _, row := range rows {
		if _, ok := required[strings.TrimSpace(row.Variant)]; ok {
			required[strings.TrimSpace(row.Variant)] = true
		}
	}
	for _, present := range required {
		if !present {
			return false
		}
	}
	return true
}
