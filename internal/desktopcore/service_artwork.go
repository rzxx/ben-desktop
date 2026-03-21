package desktopcore

import (
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	apitypes "ben/desktop/api/types"
	_ "golang.org/x/image/webp"
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

type ArtworkSourceCandidate struct {
	AudioPath   string
	SourceKind  string
	SourceRef   string
	ImagePath   string
	Width       int
	Height      int
	Bytes       int64
	cleanupFunc func()
}

func (c ArtworkSourceCandidate) Close() {
	if c.cleanupFunc != nil {
		c.cleanupFunc()
	}
}

type ArtworkBuilder interface {
	BuildForAudio(ctx context.Context, audioPath string) (ArtworkBuildResult, error)
}

type artworkEvaluatingBuilder interface {
	EvaluateForAudio(ctx context.Context, audioPath string) ([]ArtworkSourceCandidate, error)
	BuildFromSource(ctx context.Context, source ArtworkSourceCandidate) (ArtworkBuildResult, error)
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
	sources, err := b.EvaluateForAudio(ctx, audioPath)
	if err != nil {
		return ArtworkBuildResult{}, err
	}
	defer closeArtworkSourceCandidates(sources)

	best, ok := bestArtworkSourceCandidate(sources)
	if !ok {
		return ArtworkBuildResult{}, ErrNoArtworkFound
	}
	return b.BuildFromSource(ctx, best)
}

func (b *ffmpegArtworkBuilder) EvaluateForAudio(ctx context.Context, audioPath string) ([]ArtworkSourceCandidate, error) {
	candidates := make([]ArtworkSourceCandidate, 0, 2)
	addCandidate := func(kind, ref, imagePath string, cleanup func()) error {
		candidate, err := newArtworkSourceCandidate(audioPath, kind, ref, imagePath, cleanup)
		if err != nil {
			if cleanup != nil {
				cleanup()
			}
			return err
		}
		candidates = append(candidates, candidate)
		return nil
	}

	sidecar := findSidecarImage(audioPath)
	if sidecar != "" {
		if err := addCandidate("sidecar", sidecar, sidecar, nil); err != nil {
			return nil, err
		}
	}

	tempDir, err := os.MkdirTemp("", "ben-cover-*")
	if err != nil {
		closeArtworkSourceCandidates(candidates)
		return nil, fmt.Errorf("create artwork temp dir: %w", err)
	}
	embeddedOut := filepath.Join(tempDir, "embedded.jpg")
	embeddedArgs := []string{"-hide_banner", "-loglevel", "error", "-y", "-i", audioPath, "-an", "-map", "0:v:0", "-frames:v", "1", embeddedOut}
	if err := runFFmpeg(ctx, b.ffmpegPath, embeddedArgs); err == nil {
		if st, statErr := os.Stat(embeddedOut); statErr == nil && st.Size() > 0 {
			if err := addCandidate("embedded", audioPath, embeddedOut, func() { _ = os.RemoveAll(tempDir) }); err != nil {
				closeArtworkSourceCandidates(candidates)
				return nil, err
			}
		} else {
			_ = os.RemoveAll(tempDir)
		}
	} else {
		_ = os.RemoveAll(tempDir)
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("%w for %s", ErrNoArtworkFound, audioPath)
	}
	return candidates, nil
}

func newArtworkSourceCandidate(audioPath, kind, ref, imagePath string, cleanup func()) (ArtworkSourceCandidate, error) {
	imagePath = filepath.Clean(strings.TrimSpace(imagePath))
	if imagePath == "" {
		return ArtworkSourceCandidate{}, fmt.Errorf("artwork image path is required")
	}
	info, err := os.Stat(imagePath)
	if err != nil {
		return ArtworkSourceCandidate{}, err
	}
	candidate := ArtworkSourceCandidate{
		AudioPath:   filepath.Clean(strings.TrimSpace(audioPath)),
		SourceKind:  strings.TrimSpace(kind),
		SourceRef:   filepath.Clean(strings.TrimSpace(ref)),
		ImagePath:   imagePath,
		Bytes:       info.Size(),
		cleanupFunc: cleanup,
	}
	if cfg, err := decodeArtworkConfig(imagePath); err == nil {
		candidate.Width = cfg.Width
		candidate.Height = cfg.Height
	}
	return candidate, nil
}

func decodeArtworkConfig(path string) (image.Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return image.Config{}, err
	}
	defer file.Close()
	cfg, _, err := image.DecodeConfig(file)
	if err != nil {
		return image.Config{}, err
	}
	return cfg, nil
}

func (b *ffmpegArtworkBuilder) BuildFromSource(ctx context.Context, source ArtworkSourceCandidate) (ArtworkBuildResult, error) {
	if strings.TrimSpace(source.ImagePath) == "" {
		return ArtworkBuildResult{}, ErrNoArtworkFound
	}
	variants := make([]GeneratedArtworkVariant, 0, len(b.variants))
	for _, spec := range b.variants {
		data, err := b.renderVariant(ctx, source.ImagePath, spec)
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
		SourceKind: source.SourceKind,
		SourceRef:  source.SourceRef,
		Variants:   variants,
	}, nil
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
	chosenSource := ""
	chosenSourceRef := ""
	if len(existing) > 0 {
		chosenSource, chosenSourceRef, _, err = localArtworkSourceRefForScopeTx(s.app.storage.WithContext(ctx), local.LibraryID, "album", albumID, existing[0].Variant)
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

	if shouldReuseExistingAlbumArtwork(existing, chosenSource, chosenSourceRef, candidates) {
		return nil
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

func shouldReuseExistingAlbumArtwork(existing []ArtworkVariant, chosenSource, chosenSourceRef string, candidates []SourceFileModel) bool {
	if len(existing) == 0 || !artworkVariantsComplete(existing) {
		return false
	}
	chosenSource = strings.TrimSpace(chosenSource)
	chosenSourceRef = filepath.Clean(strings.TrimSpace(chosenSourceRef))
	if chosenSource == "" || chosenSourceRef == "" {
		return false
	}
	if chosenSource == "embedded" {
		if preferredSidecar := findSidecarImage(chosenSourceRef); preferredSidecar != "" {
			return false
		}
	}
	artworkUpdatedAt := latestArtworkVariantUpdate(existing)
	if artworkUpdatedAt.IsZero() {
		return false
	}
	if sourcePathUpdatedAfter(chosenSourceRef, artworkUpdatedAt) {
		return false
	}
	if anyArtworkCandidateUpdatedAfter(candidates, artworkUpdatedAt) {
		return false
	}

	switch chosenSource {
	case "embedded":
		return albumHasCandidatePath(candidates, chosenSourceRef)
	case "sidecar":
		return albumHasCandidateSidecar(candidates, chosenSourceRef)
	default:
		return false
	}
}

func anyArtworkCandidateUpdatedAfter(candidates []SourceFileModel, updatedAt time.Time) bool {
	for _, candidate := range candidates {
		candidateUpdatedAt := latestNonZeroTime(candidate.UpdatedAt, candidate.LastSeenAt, candidate.CreatedAt)
		if candidateUpdatedAt.After(updatedAt) {
			return true
		}
	}
	return false
}

func albumHasCandidatePath(candidates []SourceFileModel, wantPath string) bool {
	wantPath = filepath.Clean(strings.TrimSpace(wantPath))
	if wantPath == "" {
		return false
	}
	for _, candidate := range candidates {
		path := filepath.Clean(strings.TrimSpace(candidate.LocalPath))
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if path == wantPath {
			return true
		}
	}
	return false
}

func albumHasCandidateSidecar(candidates []SourceFileModel, wantPath string) bool {
	wantPath = filepath.Clean(strings.TrimSpace(wantPath))
	if wantPath == "" {
		return false
	}
	for _, candidate := range candidates {
		path := filepath.Clean(strings.TrimSpace(candidate.LocalPath))
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if findSidecarImage(path) == wantPath {
			return true
		}
	}
	return false
}

func sourcePathUpdatedAfter(path string, updatedAt time.Time) bool {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || updatedAt.IsZero() {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.ModTime().UTC().After(updatedAt)
}

func latestArtworkVariantUpdate(rows []ArtworkVariant) time.Time {
	var updatedAt time.Time
	for _, row := range rows {
		updatedAt = latestNonZeroTime(updatedAt, row.UpdatedAt)
	}
	return updatedAt
}

func (s *ArtworkService) buildAlbumArtwork(ctx context.Context, candidates []SourceFileModel) (ArtworkBuildResult, error) {
	if builder, ok := s.builder.(artworkEvaluatingBuilder); ok {
		return s.buildAlbumArtworkFromBestSource(ctx, candidates, builder)
	}
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

func (s *ArtworkService) buildAlbumArtworkFromBestSource(ctx context.Context, candidates []SourceFileModel, builder artworkEvaluatingBuilder) (ArtworkBuildResult, error) {
	allSources := make([]ArtworkSourceCandidate, 0, len(candidates))
	defer closeArtworkSourceCandidates(allSources)

	for _, candidate := range candidates {
		path := filepath.Clean(strings.TrimSpace(candidate.LocalPath))
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		sources, err := builder.EvaluateForAudio(ctx, path)
		if err == nil {
			allSources = append(allSources, sources...)
			continue
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

	best, ok := bestArtworkSourceCandidate(allSources)
	if !ok {
		return ArtworkBuildResult{}, ErrNoArtworkFound
	}
	return builder.BuildFromSource(ctx, best)
}

func closeArtworkSourceCandidates(candidates []ArtworkSourceCandidate) {
	for _, candidate := range candidates {
		candidate.Close()
	}
}

func bestArtworkSourceCandidate(candidates []ArtworkSourceCandidate) (ArtworkSourceCandidate, bool) {
	var best ArtworkSourceCandidate
	hasBest := false
	for _, candidate := range candidates {
		if !hasBest || artworkSourceCandidateBetter(candidate, best) {
			best = candidate
			hasBest = true
		}
	}
	return best, hasBest
}

func artworkSourceCandidateBetter(candidate, current ArtworkSourceCandidate) bool {
	candidateHasDims := candidate.Width > 0 && candidate.Height > 0
	currentHasDims := current.Width > 0 && current.Height > 0
	if candidateHasDims != currentHasDims {
		return candidateHasDims
	}

	candidateArea := int64(candidate.Width) * int64(candidate.Height)
	currentArea := int64(current.Width) * int64(current.Height)
	if candidateArea != currentArea {
		return candidateArea > currentArea
	}

	candidateMinDim := min(candidate.Width, candidate.Height)
	currentMinDim := min(current.Width, current.Height)
	if candidateMinDim != currentMinDim {
		return candidateMinDim > currentMinDim
	}

	candidateMaxDim := max(candidate.Width, candidate.Height)
	currentMaxDim := max(current.Width, current.Height)
	if candidateMaxDim != currentMaxDim {
		return candidateMaxDim > currentMaxDim
	}

	if candidate.Bytes != current.Bytes {
		return candidate.Bytes > current.Bytes
	}

	if artworkSourcePriority(candidate.SourceKind) != artworkSourcePriority(current.SourceKind) {
		return artworkSourcePriority(candidate.SourceKind) > artworkSourcePriority(current.SourceKind)
	}

	return false
}

func artworkSourcePriority(kind string) int {
	switch strings.TrimSpace(kind) {
	case "sidecar":
		return 1
	case "embedded":
		return 0
	default:
		return 0
	}
}

func (s *ArtworkService) storeArtworkScope(ctx context.Context, local apitypes.LocalContext, scopeType, scopeID string, built ArtworkBuildResult) error {
	if len(built.Variants) == 0 {
		return ErrNoArtworkFound
	}
	now := time.Now().UTC()
	return s.app.storage.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, variant := range built.Variants {
			blobID, err := s.app.blobs.StoreArtworkBytes(variant.Bytes, variant.FileExt)
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
	return s.app.storage.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return s.app.deleteArtworkScopeTx(tx, local, "album", albumID)
	})
}

func (s *ArtworkService) loadScopeArtwork(ctx context.Context, libraryID, scopeType, scopeID string) ([]ArtworkVariant, error) {
	var rows []ArtworkVariant
	if err := s.app.storage.WithContext(ctx).
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
ORDER BY sf.quality_rank DESC, sf.updated_at DESC, sf.last_seen_at DESC, sf.local_path ASC, sf.source_file_id ASC`
	if err := s.app.storage.WithContext(ctx).Raw(query, strings.TrimSpace(libraryID), strings.TrimSpace(deviceID), strings.TrimSpace(albumID)).Scan(&rows).Error; err != nil {
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
	if err := s.app.storage.WithContext(ctx).
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
	if err := s.app.storage.WithContext(ctx).
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
