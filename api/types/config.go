package apitypes

import "time"

// Logger is intentionally tiny so UI/CLI/desktop hosts can plug in any logger.
type Logger interface {
	Printf(format string, args ...any)
	Errorf(format string, args ...any)
}

// Config defines process-level defaults for the core runtime.
// Per-operation overrides live on request structs.
type Config struct {
	DBPath           string
	BlobRoot         string
	IdentityKeyPath  string
	FFmpegPath       string
	TranscodeProfile string
	CacheBytes       int64
	LogIngestErrors  bool
	Runtime          RuntimeConfig
	Network          NetworkConfig
	Scanner          ScannerConfig
	Logger           Logger
}

type RuntimeConfig struct {
	AutoStart   *bool
	StopTimeout time.Duration
}

type NetworkConfig struct {
	Enabled                        *bool
	ListenAddrs                    []string
	KnownPeers                     []string
	RelayBootstrapAddrs            []string
	RegistryURL                    string
	EnableLANDiscovery             *bool
	RequireDirectForLargeTransfers *bool
	AllowedDevices                 []string
	ServiceTag                     string
	DisableRelay                   bool
	AutoApproveInvites             bool
	InviteDefaultRole              string
	StrictInitialDial              bool
	SyncInterval                   time.Duration
	MaxOpsPerSync                  int
}

type ScannerConfig struct {
	Enabled         *bool
	InitialRescan   *bool
	Debounce        time.Duration
	MetadataWorkers int
	ArtworkWorkers  int
}
