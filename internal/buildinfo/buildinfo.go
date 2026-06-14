package buildinfo

import "strings"

var (
	appVersion       = "0.1.0-dev"
	buildCommit      = "unknown"
	buildTime        = "unknown"
	githubRepository = "rzxx/ben-desktop"
)

type Info struct {
	AppVersion       string `json:"appVersion"`
	BuildCommit      string `json:"buildCommit"`
	BuildTime        string `json:"buildTime"`
	GitHubRepository string `json:"githubRepository"`
}

func Current() Info {
	return Info{
		AppVersion:       Version(),
		BuildCommit:      clean(buildCommit, "unknown"),
		BuildTime:        clean(buildTime, "unknown"),
		GitHubRepository: clean(githubRepository, "rzxx/ben-desktop"),
	}
}

func Version() string {
	version := strings.TrimSpace(appVersion)
	version = strings.TrimPrefix(version, "v")
	if version == "" {
		return "0.1.0-dev"
	}
	return version
}

func ReleaseTag() string {
	return "v" + Version()
}

func Repository() string {
	return clean(githubRepository, "rzxx/ben-desktop")
}

func IsReleaseVersion() bool {
	version := Version()
	if strings.Contains(version, "dev") || strings.Contains(version, "dirty") {
		return false
	}
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

func clean(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
