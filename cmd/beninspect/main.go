package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"ben/desktop/internal/desktopcore"
)

type boolFlag struct {
	set   bool
	value bool
}

func (b *boolFlag) Set(value string) error {
	b.set = true
	switch value {
	case "true", "1":
		b.value = true
	case "false", "0":
		b.value = false
	default:
		return fmt.Errorf("invalid bool %q", value)
	}
	return nil
}

func (b *boolFlag) String() string {
	if !b.set {
		return ""
	}
	if b.value {
		return "true"
	}
	return "false"
}

type commonFlags struct {
	db      string
	blobRoot string
	libraryID string
	deviceID  string
	profile   string
	network   boolFlag
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) < 2 {
		return writeError(nil, errors.New("usage: beninspect <env|music|cache> <command> [flags]"))
	}

	ctx := context.Background()
	switch args[0] {
	case "env":
		if args[1] != "resolve-context" {
			return writeError(nil, fmt.Errorf("unknown env command %q", args[1]))
		}
		common, fs := newCommonFlagSet("env resolve-context")
		if err := fs.Parse(args[2:]); err != nil {
			return writeError(nil, err)
		}
		inspector, err := desktopcore.OpenInspector(desktopcore.InspectConfig{
			DBPath:           common.db,
			BlobRoot:         common.blobRoot,
			PreferredProfile: common.profile,
		})
		if err != nil {
			return writeError(nil, err)
		}
		defer func() { _ = inspector.Close() }()
		req := desktopcore.ResolveInspectContextRequest{
			LibraryID:        common.libraryID,
			DeviceID:         common.deviceID,
			PreferredProfile: common.profile,
		}
		if common.network.set {
			req.NetworkRunning = &common.network.value
		}
		resolution, err := inspector.ResolveContext(ctx, req)
		return writeJSON(resolution, err)

	case "music":
		if len(args) < 2 {
			return writeError(nil, errors.New("music command is required"))
		}
		switch args[1] {
		case "trace-recording":
			common, fs := newCommonFlagSet("music trace-recording")
			id := fs.String("id", "", "recording id")
			if err := fs.Parse(args[2:]); err != nil {
				return writeError(nil, err)
			}
			inspector, err := openInspector(common)
			if err != nil {
				return writeError(nil, err)
			}
			defer func() { _ = inspector.Close() }()
			trace, err := inspector.TraceRecording(ctx, desktopcore.TraceRecordingRequest{
				ID: *id,
				ResolveInspectContextRequest: resolveRequestFromFlags(common),
			})
			return writeJSON(trace, err)
		case "trace-album":
			common, fs := newCommonFlagSet("music trace-album")
			id := fs.String("id", "", "album id")
			if err := fs.Parse(args[2:]); err != nil {
				return writeError(nil, err)
			}
			inspector, err := openInspector(common)
			if err != nil {
				return writeError(nil, err)
			}
			defer func() { _ = inspector.Close() }()
			trace, err := inspector.TraceAlbum(ctx, desktopcore.TraceAlbumRequest{
				ID: *id,
				ResolveInspectContextRequest: resolveRequestFromFlags(common),
			})
			return writeJSON(trace, err)
		case "trace-context":
			common, fs := newCommonFlagSet("music trace-context")
			kind := fs.String("kind", "", "context kind")
			id := fs.String("id", "", "context id")
			if err := fs.Parse(args[2:]); err != nil {
				return writeError(nil, err)
			}
			inspector, err := openInspector(common)
			if err != nil {
				return writeError(nil, err)
			}
			defer func() { _ = inspector.Close() }()
			trace, err := inspector.TracePlaybackContext(ctx, desktopcore.TracePlaybackContextRequest{
				Kind: *kind,
				ID:   *id,
				ResolveInspectContextRequest: resolveRequestFromFlags(common),
			})
			return writeJSON(trace, err)
		default:
			return writeError(nil, fmt.Errorf("unknown music command %q", args[1]))
		}

	case "cache":
		if len(args) < 2 {
			return writeError(nil, errors.New("cache command is required"))
		}
		switch args[1] {
		case "trace-recording":
			common, fs := newCommonFlagSet("cache trace-recording")
			id := fs.String("id", "", "recording id")
			if err := fs.Parse(args[2:]); err != nil {
				return writeError(nil, err)
			}
			inspector, err := openInspector(common)
			if err != nil {
				return writeError(nil, err)
			}
			defer func() { _ = inspector.Close() }()
			trace, err := inspector.TraceRecordingCache(ctx, desktopcore.TraceRecordingCacheRequest{
				ID: *id,
				ResolveInspectContextRequest: resolveRequestFromFlags(common),
			})
			return writeJSON(trace, err)
		case "trace-blob":
			common, fs := newCommonFlagSet("cache trace-blob")
			blobID := fs.String("blob-id", "", "blob id")
			if err := fs.Parse(args[2:]); err != nil {
				return writeError(nil, err)
			}
			inspector, err := openInspector(common)
			if err != nil {
				return writeError(nil, err)
			}
			defer func() { _ = inspector.Close() }()
			trace, err := inspector.TraceBlob(ctx, desktopcore.TraceBlobRequest{
				BlobID: *blobID,
				ResolveInspectContextRequest: resolveRequestFromFlags(common),
			})
			return writeJSON(trace, err)
		default:
			return writeError(nil, fmt.Errorf("unknown cache command %q", args[1]))
		}
	default:
		return writeError(nil, fmt.Errorf("unknown command %q", args[0]))
	}
}

func newCommonFlagSet(name string) (*commonFlags, *flag.FlagSet) {
	common := &commonFlags{}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&common.db, "db", "", "sqlite db path")
	fs.StringVar(&common.blobRoot, "blob-root", "", "blob root")
	fs.StringVar(&common.libraryID, "library-id", "", "library id")
	fs.StringVar(&common.deviceID, "device-id", "", "device id")
	fs.StringVar(&common.profile, "profile", "", "preferred profile")
	fs.Var(&common.network, "network-running", "network running override")
	return common, fs
}

func openInspector(common *commonFlags) (*desktopcore.Inspector, error) {
	return desktopcore.OpenInspector(desktopcore.InspectConfig{
		DBPath:           common.db,
		BlobRoot:         common.blobRoot,
		PreferredProfile: common.profile,
	})
}

func resolveRequestFromFlags(common *commonFlags) desktopcore.ResolveInspectContextRequest {
	req := desktopcore.ResolveInspectContextRequest{
		LibraryID:        common.libraryID,
		DeviceID:         common.deviceID,
		PreferredProfile: common.profile,
	}
	if common.network.set {
		req.NetworkRunning = &common.network.value
	}
	return req
}

func writeJSON(value any, err error) int {
	if err != nil {
		return writeError(value, err)
	}
	payload, marshalErr := json.MarshalIndent(value, "", "  ")
	if marshalErr != nil {
		return writeError(nil, marshalErr)
	}
	payload = append(payload, '\n')
	_, _ = os.Stdout.Write(payload)
	return 0
}

func writeError(value any, err error) int {
	envelope := map[string]any{
		"schema_version": 1,
		"error":          err.Error(),
	}
	if value != nil {
		payload, marshalErr := json.Marshal(value)
		if marshalErr == nil {
			var parsed map[string]any
			if err := json.Unmarshal(payload, &parsed); err == nil {
				for key, value := range parsed {
					envelope[key] = value
				}
			}
		}
	}
	payload, _ := json.MarshalIndent(envelope, "", "  ")
	payload = append(payload, '\n')
	_, _ = os.Stdout.Write(payload)
	return 1
}
