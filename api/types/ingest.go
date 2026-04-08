package apitypes

import (
	"context"
)

type ScanStats struct {
	Scanned          int
	SkippedUnchanged int
	Imported         int
	Errors           int
}

type IngestSurface interface {
	RepairLibrary(ctx context.Context) (ScanStats, error)
	SetScanRoots(ctx context.Context, roots []string) error
	AddScanRoots(ctx context.Context, roots []string) ([]string, error)
	RemoveScanRoots(ctx context.Context, roots []string) ([]string, error)
	ScanRoots(ctx context.Context) ([]string, error)
}
