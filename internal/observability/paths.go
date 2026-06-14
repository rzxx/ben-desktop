package observability

import (
	"os"
	"path/filepath"
	"strings"
)

type Paths struct {
	Root      string
	Logs      string
	Traces    string
	Support   string
	RecentLog string
}

func DefaultPaths(appName string) (Paths, error) {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		appName = "ben-desktop"
	}
	root, err := os.UserCacheDir()
	if err != nil {
		return Paths{}, err
	}
	root = filepath.Join(root, appName, "observability")
	paths := Paths{
		Root:      root,
		Logs:      filepath.Join(root, "logs"),
		Traces:    filepath.Join(root, "traces"),
		Support:   filepath.Join(root, "support-bundles"),
		RecentLog: filepath.Join(root, "logs", "app.jsonl"),
	}
	for _, path := range []string{paths.Root, paths.Logs, paths.Traces, paths.Support} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return Paths{}, err
		}
	}
	return paths, nil
}
