package desktopcore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const healthDurationToleranceSeconds = 0.5

type inspectMediaResult struct {
	ProbeDurationSeconds   *float64
	ProbeError             string
	DecodedDurationSeconds *float64
	DecodeError            string
}

type inspectMediaChecker interface {
	Check(ctx context.Context, path string, decode bool) (inspectMediaResult, error)
}

type ffmpegInspectMediaChecker struct {
	ffmpegPath  string
	ffprobePath string
}

func newInspectMediaChecker(ffmpegPath string) inspectMediaChecker {
	ffmpegPath = strings.TrimSpace(ffmpegPath)
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	return &ffmpegInspectMediaChecker{
		ffmpegPath:  ffmpegPath,
		ffprobePath: companionBinaryPath(ffmpegPath, "ffmpeg", "ffprobe"),
	}
}

func companionBinaryPath(primaryPath, primaryName, secondaryName string) string {
	primaryPath = strings.TrimSpace(primaryPath)
	if primaryPath == "" {
		return secondaryName
	}
	base := strings.ToLower(filepath.Base(primaryPath))
	expected := strings.ToLower(primaryName)
	if base != expected && base != expected+".exe" {
		return secondaryName
	}
	dir := filepath.Dir(primaryPath)
	ext := filepath.Ext(primaryPath)
	if dir == "." || dir == "" {
		if ext == "" {
			return secondaryName
		}
		return secondaryName + ext
	}
	return filepath.Join(dir, secondaryName+ext)
}

func (c *ffmpegInspectMediaChecker) Check(ctx context.Context, path string, decode bool) (inspectMediaResult, error) {
	result := inspectMediaResult{}

	probeValue, probeErr := runInspectCommand(ctx, c.ffprobePath,
		[]string{"-v", "error", "-show_entries", "format=duration", "-of", "default=nw=1:nk=1", path},
		false,
	)
	if probeErr != nil {
		if errors.Is(probeErr, context.Canceled) || errors.Is(probeErr, context.DeadlineExceeded) {
			return inspectMediaResult{}, probeErr
		}
		result.ProbeError = probeErr.Error()
	} else {
		parsed, err := strconv.ParseFloat(strings.TrimSpace(probeValue), 64)
		if err != nil {
			result.ProbeError = fmt.Sprintf("parse ffprobe duration: %v", err)
		} else {
			result.ProbeDurationSeconds = &parsed
		}
	}

	if !decode {
		return result, nil
	}

	progress, decodeErr := runInspectCommand(ctx, c.ffmpegPath,
		[]string{"-v", "error", "-i", path, "-map", "0:a:0", "-f", "null", "-", "-progress", "pipe:1", "-nostats"},
		true,
	)
	if decodeErr != nil {
		if errors.Is(decodeErr, context.Canceled) || errors.Is(decodeErr, context.DeadlineExceeded) {
			return inspectMediaResult{}, decodeErr
		}
		result.DecodeError = decodeErr.Error()
		return result, nil
	}

	if decodedSeconds, ok := parseDecodedDuration(progress); ok {
		result.DecodedDurationSeconds = &decodedSeconds
	}
	return result, nil
}

func runInspectCommand(ctx context.Context, command string, args []string, captureStdout bool) (string, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	configureBackgroundProcess(cmd)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	output := stdout.Bytes()
	if err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
			return "", fmt.Errorf("%s executable %q not found: %w", filepath.Base(command), command, err)
		}
		errText := strings.TrimSpace(stderr.String())
		if errText == "" {
			errText = strings.TrimSpace(string(output))
		}
		if errText == "" {
			return "", fmt.Errorf("%s failed: %w", filepath.Base(command), err)
		}
		return "", fmt.Errorf("%s failed: %w (%s)", filepath.Base(command), err, errText)
	}
	if !captureStdout {
		return strings.TrimSpace(string(output)), nil
	}
	return strings.TrimSpace(string(output)), nil
}

func parseDecodedDuration(progress string) (float64, bool) {
	for _, line := range strings.Split(progress, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "out_time_ms=") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "out_time_ms="))
		if raw == "" {
			continue
		}
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			continue
		}
		return float64(value) / 1_000_000, true
	}
	return 0, false
}

type inspectHealthSourceRow struct {
	SourceFileModel
	Title string
}

func (i *Inspector) HealthCheck(ctx context.Context, req HealthCheckRequest) (HealthCheckReport, error) {
	report := HealthCheckReport{
		SchemaVersion: inspectSchemaVersion,
		Request:       inspectorRequest(req),
		Problems:      []SourceFileHealthItem{},
		Checked:       []SourceFileHealthItem{},
	}
	report.Summary.DecodeEnabled = req.Decode
	report.Summary.FilesystemCompared = req.IncludeFilesystem
	report.Summary.Limit = req.Limit
	report.Summary.DateFilter = strings.TrimSpace(req.Date)

	resolution, local, err := i.resolveLocalContext(ctx, req.ResolveInspectContextRequest)
	report.Context = resolution
	if err != nil {
		return report, err
	}

	dateFilter := strings.TrimSpace(req.Date)
	if dateFilter != "" {
		if _, err := time.Parse("2006-01-02", dateFilter); err != nil {
			return report, fmt.Errorf("invalid date %q: expected YYYY-MM-DD", dateFilter)
		}
	}

	rows, err := i.loadHealthSourceFilesForDevice(ctx, local.LibraryID, local.DeviceID)
	if err != nil {
		return report, err
	}

	presentPaths := make(map[string]struct{}, len(rows))
	filtered := make([]inspectHealthSourceRow, 0, len(rows))
	for _, row := range rows {
		cleanPath := filepath.Clean(strings.TrimSpace(row.LocalPath))
		if cleanPath != "" {
			presentPaths[cleanPath] = struct{}{}
		}
		if !matchesHealthDate(row.SourceFileModel, dateFilter) {
			continue
		}
		filtered = append(filtered, row)
	}

	sort.Slice(filtered, func(left, right int) bool {
		leftTime := healthIndexedAt(filtered[left].SourceFileModel)
		rightTime := healthIndexedAt(filtered[right].SourceFileModel)
		if !leftTime.Equal(rightTime) {
			return leftTime.After(rightTime)
		}
		return filtered[left].LocalPath < filtered[right].LocalPath
	})

	report.Summary.CandidateCount = len(filtered)
	if req.Limit > 0 && len(filtered) > req.Limit {
		filtered = filtered[:req.Limit]
	}

	checker := i.mediaChecker
	if checker == nil && req.Decode {
		checker = newInspectMediaChecker(strings.TrimSpace(i.app.cfg.FFmpegPath))
	}

	for _, row := range filtered {
		item, err := i.inspectHealthItem(ctx, row, req.Decode, checker)
		if err != nil {
			return report, err
		}
		report.Checked = append(report.Checked, item)
		if item.Status == "ok" {
			report.Summary.OKCount++
			continue
		}
		report.Problems = append(report.Problems, item)
	}

	if req.IncludeFilesystem {
		missing, err := i.healthMissingFromDB(ctx, local.LibraryID, local.DeviceID, dateFilter, presentPaths)
		if err != nil {
			return report, err
		}
		report.MissingFromDB = missing
	}

	report.Summary.CheckedCount = len(report.Checked)
	report.Summary.ProblemCount = len(report.Problems)
	report.Summary.MissingFromDBCount = len(report.MissingFromDB)
	return report, nil
}

func (i *Inspector) loadHealthSourceFilesForDevice(ctx context.Context, libraryID, deviceID string) ([]inspectHealthSourceRow, error) {
	var rows []inspectHealthSourceRow
	query := `
SELECT
	sf.*,
	COALESCE(tv.title, '') AS title
FROM source_files sf
LEFT JOIN track_variants tv
	ON tv.library_id = sf.library_id AND tv.track_variant_id = sf.track_variant_id
WHERE sf.library_id = ? AND sf.device_id = ? AND sf.is_present = 1
ORDER BY sf.created_at DESC, sf.updated_at DESC, sf.last_seen_at DESC, sf.local_path ASC`
	if err := i.app.storage.WithContext(ctx).Raw(query, libraryID, deviceID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func matchesHealthDate(row SourceFileModel, dateFilter string) bool {
	dateFilter = strings.TrimSpace(dateFilter)
	if dateFilter == "" {
		return true
	}
	indexedAt := healthIndexedAt(row)
	if indexedAt.IsZero() {
		return false
	}
	return indexedAt.In(time.Local).Format("2006-01-02") == dateFilter
}

func healthIndexedAt(row SourceFileModel) time.Time {
	switch {
	case !row.CreatedAt.IsZero():
		return row.CreatedAt
	case !row.UpdatedAt.IsZero():
		return row.UpdatedAt
	default:
		return row.LastSeenAt
	}
}

func (i *Inspector) inspectHealthItem(ctx context.Context, row inspectHealthSourceRow, decode bool, checker inspectMediaChecker) (SourceFileHealthItem, error) {
	item := SourceFileHealthItem{
		SourceFileID:   row.SourceFileID,
		TrackVariantID: row.TrackVariantID,
		Title:          strings.TrimSpace(row.Title),
		LocalPath:      row.LocalPath,
		DBSizeBytes:    row.SizeBytes,
		DBDurationMS:   row.DurationMS,
		Status:         "ok",
	}
	if indexedAt := healthIndexedAt(row.SourceFileModel); !indexedAt.IsZero() {
		item.IndexedAt = indexedAt.UTC().Format(time.RFC3339Nano)
	}

	info, err := os.Stat(row.LocalPath)
	if err != nil {
		if os.IsNotExist(err) {
			item.Problems = append(item.Problems, "missing")
		} else {
			item.Problems = append(item.Problems, "stat_failed")
			item.DecodeError = err.Error()
		}
		item.Status = "problem"
		return item, nil
	}
	item.Exists = true
	size := info.Size()
	item.ActualSizeBytes = &size
	if size != row.SizeBytes {
		item.Problems = append(item.Problems, "size_changed_since_scan")
	}

	if decode && checker != nil {
		media, err := checker.Check(ctx, row.LocalPath, true)
		if err != nil {
			return SourceFileHealthItem{}, err
		}
		item.ProbeDurationSeconds = media.ProbeDurationSeconds
		item.DecodedDurationSeconds = media.DecodedDurationSeconds
		item.ProbeError = media.ProbeError
		item.DecodeError = media.DecodeError

		if media.ProbeError != "" {
			item.Problems = append(item.Problems, "probe_failed")
		}
		if media.DecodeError != "" {
			item.Problems = append(item.Problems, "decode_failed")
		}
		if media.ProbeDurationSeconds == nil {
			item.Problems = append(item.Problems, "no_probe_duration")
		}
		if media.DecodedDurationSeconds == nil {
			item.Problems = append(item.Problems, "no_decoded_duration")
		}
		if media.ProbeDurationSeconds != nil && media.DecodedDurationSeconds != nil {
			delta := *media.ProbeDurationSeconds - *media.DecodedDurationSeconds
			delta = math.Round(delta*1_000_000) / 1_000_000
			item.DurationDeltaSeconds = &delta
			if math.Abs(delta) > healthDurationToleranceSeconds {
				item.Problems = append(item.Problems, "decode_duration_mismatch")
			}
		}
	}

	if len(item.Problems) > 0 {
		item.Status = "problem"
	}
	return item, nil
}

func (i *Inspector) healthMissingFromDB(ctx context.Context, libraryID, deviceID, dateFilter string, presentPaths map[string]struct{}) ([]string, error) {
	roots, err := i.app.scanRootsForDevice(ctx, libraryID, deviceID)
	if err != nil {
		return nil, err
	}
	missing := make([]string, 0)
	seen := make(map[string]struct{})
	for _, root := range roots {
		paths, err := enumerateAudioPaths(ctx, root)
		if err != nil {
			return nil, err
		}
		for _, path := range paths {
			cleanPath := filepath.Clean(path)
			if _, ok := seen[cleanPath]; ok {
				continue
			}
			seen[cleanPath] = struct{}{}
			if !matchesFilesystemDate(cleanPath, dateFilter) {
				continue
			}
			if _, ok := presentPaths[cleanPath]; ok {
				continue
			}
			missing = append(missing, cleanPath)
		}
	}
	sort.Strings(missing)
	return missing, nil
}

func matchesFilesystemDate(path, dateFilter string) bool {
	dateFilter = strings.TrimSpace(dateFilter)
	if dateFilter == "" {
		return true
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.ModTime().In(time.Local).Format("2006-01-02") == dateFilter
}
