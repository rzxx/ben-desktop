package main

import (
	"ben/registryauth"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/libp2p/go-libp2p"
	crypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
	relayv2 "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/time/rate"
	_ "modernc.org/sqlite"
)

const (
	defaultHTTPAddr        = ":8787"
	defaultDBPath          = "ben-relayd.db"
	defaultIdentityKeyPath = "ben-relayd.identity.key"
	defaultMaxBodyBytes    = int64(1 << 20)
	sqliteBusyTimeout      = 5 * time.Second
	maxPresenceAddrs       = 16
	maxPresenceAddrsBytes  = 8 << 10
	currentSchemaVersion   = 3
)

const (
	envPort            = "PORT"
	envHTTPAddr        = "RELAYD_HTTP_ADDR"
	envDBPath          = "RELAYD_DB_PATH"
	envIdentityKeyPath = "RELAYD_IDENTITY_KEY_PATH"
	envRailwayVolume   = "RAILWAY_VOLUME_MOUNT_PATH"
	envPeerListenAddrs = "RELAYD_PEER_LISTEN_ADDRS"
	envAdvertiseAddrs  = "RELAYD_ADVERTISE_ADDRS"
	envTLSCertPath     = "RELAYD_TLS_CERT_PATH"
	envTLSKeyPath      = "RELAYD_TLS_KEY_PATH"
	envTrustedProxies  = "RELAYD_TRUSTED_PROXIES"
	envClientIPHeader  = "RELAYD_CLIENT_IP_HEADER"
)

var defaultPeerListenAddrs = []string{
	"/ip4/0.0.0.0/tcp/4001",
	"/ip6/::/tcp/4001",
	"/ip4/0.0.0.0/udp/4001/quic-v1",
	"/ip6/::/udp/4001/quic-v1",
}

type presenceRecord = registryauth.PresenceRecord

type verifiedMembership struct {
	LibraryID     string
	DeviceID      string
	PeerID        string
	RootPublicKey string
	CertSerial    int64
	CertExpiresAt int64
}

type relaydOptions struct {
	HTTPAddr                   string
	DBPath                     string
	IdentityKeyPath            string
	PeerListenAddrs            []string
	AdvertiseAddrs             []string
	TLSCertPath                string
	TLSKeyPath                 string
	ReadHeaderTimeout          time.Duration
	ReadTimeout                time.Duration
	WriteTimeout               time.Duration
	IdleTimeout                time.Duration
	ShutdownTimeout            time.Duration
	MaxBodyBytes               int64
	RateLimitRequestsPerSecond float64
	RateLimitBurst             int
	RateLimitIdleTTL           time.Duration
	TrustedProxies             []string
	ClientIPHeader             string
	RelayACLDisabled           bool
	ReservationTTL             time.Duration
	MaxReservations            int
	MaxCircuits                int
	MaxReservationsPerPeer     int
	MaxReservationsPerIP       int
	MaxReservationsPerASN      int
	RelayLimitDuration         time.Duration
	RelayLimitDataBytes        int64
}

type relaydServer struct {
	db      *sql.DB
	host    host.Host
	metrics *relaydMetrics
}

type relaydMetrics struct {
	httpRequestsTotal       *prometheus.CounterVec
	httpRequestDuration     *prometheus.HistogramVec
	registryEventsTotal     *prometheus.CounterVec
	rateLimitRejectedTotal  prometheus.Counter
	presenceRecordsGauge    prometheus.Gauge
	memberAuthStateGauge    prometheus.Gauge
	libraryRootsGauge       prometheus.Gauge
	sqliteOperationFailures *prometheus.CounterVec
	relayACLDecisionsTotal  *prometheus.CounterVec
}

type relayACL struct {
	db      *sql.DB
	metrics *relaydMetrics
}

type statusResponseWriter struct {
	http.ResponseWriter
	status int
}

type trustedProxySet struct {
	nets []*net.IPNet
	ips  map[string]struct{}
}

type ipRateLimiter struct {
	mu       sync.Mutex
	limit    rate.Limit
	burst    int
	idleTTL  time.Duration
	visitors map[string]*rateVisitor
}

type rateVisitor struct {
	limiter    *rate.Limiter
	lastSeenAt time.Time
}

func main() {
	opts, err := parseOptions()
	if err != nil {
		log.Fatalf("parse options: %v", err)
	}

	if err := prepareProcessPrivileges(opts); err != nil {
		log.Fatalf("prepare process privileges: %v", err)
	}
	if err := prepareRegistryStorage(opts); err != nil {
		log.Fatalf("prepare registry storage: %v", err)
	}

	db, err := sql.Open("sqlite", opts.DBPath)
	if err != nil {
		log.Fatalf("open registry db: %v", err)
	}
	defer db.Close()
	configureSQLite(db)
	if err := initSchema(db); err != nil {
		log.Fatalf("init registry db: %v", err)
	}

	metrics := newRelaydMetrics(prometheus.DefaultRegisterer)
	hostNode, err := newRelayHost(opts, db, metrics)
	if err != nil {
		log.Fatalf("create relay host: %v", err)
	}
	defer hostNode.Close()

	server := &relaydServer{db: db, host: hostNode, metrics: metrics}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", server.handleHealth)
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/v1/presence/announce", server.handlePresenceAnnounce)
	mux.HandleFunc("/v1/presence/member", server.handlePresenceMember)
	mux.HandleFunc("/v1/relay/authorize", server.handleRelayAuthorize)
	mux.HandleFunc("/v1/revocations/sync", server.handleRevocationSync)
	mux.HandleFunc("/v1/invites/owner", server.handleInviteOwner)

	handler, limiter := buildHTTPHandler(mux, opts, metrics)
	httpServer := &http.Server{
		Addr:              opts.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: opts.ReadHeaderTimeout,
		ReadTimeout:       opts.ReadTimeout,
		WriteTimeout:      opts.WriteTimeout,
		IdleTimeout:       opts.IdleTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go cleanupLoop(ctx, db, metrics)
	if limiter != nil {
		go limiter.cleanupLoop(ctx)
	}
	go metricsLoop(ctx, db, metrics)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), opts.ShutdownTimeout)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	log.Printf(
		"ben-relayd listening http=%s peer=%s listenAddrs=%v advertiseAddrs=%v identityKey=%s db=%s tls=%t rateLimitRPS=%.2f rateLimitBurst=%d maxBodyBytes=%d",
		opts.HTTPAddr,
		hostNode.ID().String(),
		opts.listenAddrs(),
		formatHostAddrs(hostNode),
		opts.IdentityKeyPath,
		opts.DBPath,
		opts.TLSEnabled(),
		opts.RateLimitRequestsPerSecond,
		opts.RateLimitBurst,
		opts.MaxBodyBytes,
	)
	if err := serveHTTP(httpServer, opts); err != nil && err != http.ErrServerClosed {
		log.Fatalf("serve http: %v", err)
	}
}

func parseOptions() (relaydOptions, error) {
	return parseOptionsFromArgs(os.Args[1:])
}

func parseOptionsFromArgs(args []string) (relaydOptions, error) {
	fs := flag.NewFlagSet("ben-relayd", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var opts relaydOptions
	var peerListenAddrs string
	var advertiseAddrs string
	var trustedProxies string
	fs.StringVar(&opts.HTTPAddr, "http-addr", defaultHTTPAddrForEnv(), "HTTP listen address")
	fs.StringVar(&opts.DBPath, "db", envOrDefault(envDBPath, defaultStoragePath(defaultDBPath)), "SQLite registry database path")
	fs.StringVar(&opts.IdentityKeyPath, "identity-key", envOrDefault(envIdentityKeyPath, defaultStoragePath(defaultIdentityKeyPath)), "libp2p relay identity private key path")
	fs.StringVar(&peerListenAddrs, "peer-listen-addrs", strings.Join(envListOrDefault(envPeerListenAddrs, defaultPeerListenAddrs), ","), "comma-separated libp2p listen multiaddrs for the public relay")
	fs.StringVar(&advertiseAddrs, "advertise-addrs", strings.Join(envListOrDefault(envAdvertiseAddrs, nil), ","), "comma-separated public libp2p multiaddrs to advertise instead of the listen addresses")
	fs.StringVar(&opts.TLSCertPath, "tls-cert", envOrDefault(envTLSCertPath, ""), "optional TLS certificate path for serving the registry API directly over HTTPS")
	fs.StringVar(&opts.TLSKeyPath, "tls-key", envOrDefault(envTLSKeyPath, ""), "optional TLS private key path for serving the registry API directly over HTTPS")
	fs.DurationVar(&opts.ReadHeaderTimeout, "read-header-timeout", 5*time.Second, "HTTP read header timeout")
	fs.DurationVar(&opts.ReadTimeout, "read-timeout", 15*time.Second, "HTTP full read timeout")
	fs.DurationVar(&opts.WriteTimeout, "write-timeout", 15*time.Second, "HTTP write timeout")
	fs.DurationVar(&opts.IdleTimeout, "idle-timeout", 60*time.Second, "HTTP idle timeout")
	fs.DurationVar(&opts.ShutdownTimeout, "shutdown-timeout", 10*time.Second, "graceful shutdown timeout")
	fs.Int64Var(&opts.MaxBodyBytes, "max-body-bytes", defaultMaxBodyBytes, "maximum HTTP request body size in bytes")
	fs.Float64Var(&opts.RateLimitRequestsPerSecond, "rate-limit-rps", 10, "per-IP HTTP request rate limit in requests per second; set to 0 to disable")
	fs.IntVar(&opts.RateLimitBurst, "rate-limit-burst", 20, "per-IP HTTP request burst limit")
	fs.DurationVar(&opts.RateLimitIdleTTL, "rate-limit-idle-ttl", 10*time.Minute, "how long to retain idle per-IP rate limit state")
	fs.StringVar(&trustedProxies, "trusted-proxies", strings.Join(envListOrDefault(envTrustedProxies, nil), ","), "comma-separated trusted reverse proxy IPs or CIDRs allowed to supply the client IP header")
	fs.StringVar(&opts.ClientIPHeader, "client-ip-header", envOrDefault(envClientIPHeader, ""), "client IP header to trust only from trusted proxies, for example X-Forwarded-For")
	fs.BoolVar(&opts.RelayACLDisabled, "relay-acl-disabled", false, "disable membership-backed circuit relay ACL; intended only for local development")
	fs.DurationVar(&opts.ReservationTTL, "relay-reservation-ttl", time.Hour, "relay reservation TTL")
	fs.IntVar(&opts.MaxReservations, "relay-max-reservations", 128, "maximum relay reservations")
	fs.IntVar(&opts.MaxCircuits, "relay-max-circuits", 8, "maximum concurrent relay circuits")
	fs.IntVar(&opts.MaxReservationsPerPeer, "relay-max-reservations-per-peer", 1, "maximum reservations per peer")
	fs.IntVar(&opts.MaxReservationsPerIP, "relay-max-reservations-per-ip", 8, "maximum reservations per source IP")
	fs.IntVar(&opts.MaxReservationsPerASN, "relay-max-reservations-per-asn", 32, "maximum reservations per ASN")
	fs.DurationVar(&opts.RelayLimitDuration, "relay-limit-duration", 90*time.Second, "maximum relay circuit duration")
	fs.Int64Var(&opts.RelayLimitDataBytes, "relay-limit-data-bytes", 256<<10, "maximum relay circuit data per direction in bytes")

	if err := fs.Parse(args); err != nil {
		return relaydOptions{}, err
	}
	opts.HTTPAddr = strings.TrimSpace(opts.HTTPAddr)
	opts.DBPath = strings.TrimSpace(opts.DBPath)
	opts.IdentityKeyPath = strings.TrimSpace(opts.IdentityKeyPath)
	opts.TLSCertPath = strings.TrimSpace(opts.TLSCertPath)
	opts.TLSKeyPath = strings.TrimSpace(opts.TLSKeyPath)
	opts.PeerListenAddrs = compactNonEmptyStrings(strings.Split(peerListenAddrs, ","))
	opts.AdvertiseAddrs = compactNonEmptyStrings(strings.Split(advertiseAddrs, ","))
	opts.TrustedProxies = compactNonEmptyStrings(strings.Split(trustedProxies, ","))
	opts.ClientIPHeader = strings.TrimSpace(opts.ClientIPHeader)
	if err := opts.validate(); err != nil {
		return relaydOptions{}, err
	}
	return opts, nil
}

func newRelayHost(opts relaydOptions, db *sql.DB, metrics *relaydMetrics) (host.Host, error) {
	priv, err := loadOrCreateRelayIdentityKey(opts.IdentityKeyPath)
	if err != nil {
		return nil, fmt.Errorf("load relay identity: %w", err)
	}
	rm, err := newRelayResourceManager()
	if err != nil {
		return nil, err
	}
	relayOpts := []relayv2.Option{relayv2.WithResources(opts.relayResources())}
	if !opts.RelayACLDisabled {
		relayOpts = append(relayOpts, relayv2.WithACL(&relayACL{db: db, metrics: metrics}))
	}
	libp2pOpts := []libp2p.Option{
		libp2p.Identity(priv),
		libp2p.ListenAddrStrings(opts.listenAddrs()...),
		libp2p.ResourceManager(rm),
		libp2p.ForceReachabilityPublic(),
		libp2p.EnableRelayService(relayOpts...),
	}
	if advertiseAddrs, err := parseMultiaddrs(opts.advertiseAddrs(), "advertise address"); err != nil {
		return nil, err
	} else if len(advertiseAddrs) > 0 {
		libp2pOpts = append(libp2pOpts, libp2p.AddrsFactory(func([]ma.Multiaddr) []ma.Multiaddr {
			out := make([]ma.Multiaddr, 0, len(advertiseAddrs))
			out = append(out, advertiseAddrs...)
			return out
		}))
	}
	return libp2p.New(libp2pOpts...)
}

func (o relaydOptions) listenAddrs() []string {
	if addrs := compactNonEmptyStrings(o.PeerListenAddrs); len(addrs) > 0 {
		return addrs
	}
	return append([]string(nil), defaultPeerListenAddrs...)
}

func (o relaydOptions) advertiseAddrs() []string {
	return compactNonEmptyStrings(o.AdvertiseAddrs)
}

func newRelayResourceManager() (network.ResourceManager, error) {
	scaling := rcmgr.DefaultLimits
	libp2p.SetDefaultServiceLimits(&scaling)
	return rcmgr.NewResourceManager(rcmgr.NewFixedLimiter(scaling.AutoScale()), rcmgr.WithMetricsDisabled())
}

func (a *relayACL) AllowReserve(p peer.ID, _ ma.Multiaddr) bool {
	allowed := relayPeerAuthorized(a.db, p.String())
	a.record("reserve", allowed)
	return allowed
}

func (a *relayACL) AllowConnect(_ peer.ID, _ ma.Multiaddr, dest peer.ID) bool {
	allowed := relayPeerAuthorized(a.db, dest.String())
	a.record("connect", allowed)
	return allowed
}

func (a *relayACL) record(operation string, allowed bool) {
	if a == nil || a.metrics == nil {
		return
	}
	result := "denied"
	if allowed {
		result = "allowed"
	}
	a.metrics.relayACLDecisionsTotal.WithLabelValues(operation, result).Inc()
}

func relayPeerAuthorized(db *sql.DB, peerID string) bool {
	if db == nil || strings.TrimSpace(peerID) == "" {
		return false
	}
	var exists int
	err := db.QueryRow(`
		SELECT 1
		FROM member_auth_state
		WHERE peer_id = ?
			AND (cert_expires_at <= 0 OR cert_expires_at >= ?)
		LIMIT 1
	`, strings.TrimSpace(peerID), time.Now().UTC().UnixNano()).Scan(&exists)
	return err == nil && exists == 1
}

func loadOrCreateRelayIdentityKey(path string) (crypto.PrivKey, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("identity key path is required")
	}
	if data, err := os.ReadFile(path); err == nil {
		key, err := crypto.UnmarshalPrivateKey(data)
		if err != nil {
			return nil, fmt.Errorf("decode private key: %w", err)
		}
		return key, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read private key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create identity directory: %w", err)
	}
	priv, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate private key: %w", err)
	}
	encoded, err := crypto.MarshalPrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("encode private key: %w", err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return nil, fmt.Errorf("write private key: %w", err)
	}
	return priv, nil
}

func prepareRegistryStorage(opts relaydOptions) error {
	if err := ensureWritableParent(opts.DBPath, "registry database"); err != nil {
		return err
	}
	if err := ensureWritableParent(opts.IdentityKeyPath, "relay identity key"); err != nil {
		return err
	}
	return nil
}

func ensureWritableParent(path, description string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("%s path is required", description)
	}
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s directory %q: %w", description, dir, err)
	}
	probe, err := os.CreateTemp(dir, ".ben-relayd-write-test-*")
	if err != nil {
		return fmt.Errorf("write test %s directory %q: %w", description, dir, err)
	}
	probePath := probe.Name()
	if closeErr := probe.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if removeErr := os.Remove(probePath); removeErr != nil && err == nil {
		err = removeErr
	}
	if err != nil {
		return fmt.Errorf("write test %s directory %q: %w", description, dir, err)
	}
	return nil
}

func (o relaydOptions) relayResources() relayv2.Resources {
	resources := relayv2.DefaultResources()
	resources.ReservationTTL = o.ReservationTTL
	resources.MaxReservations = o.MaxReservations
	resources.MaxCircuits = o.MaxCircuits
	resources.MaxReservationsPerPeer = o.MaxReservationsPerPeer
	resources.MaxReservationsPerIP = o.MaxReservationsPerIP
	resources.MaxReservationsPerASN = o.MaxReservationsPerASN
	resources.Limit = &relayv2.RelayLimit{
		Duration: o.RelayLimitDuration,
		Data:     o.RelayLimitDataBytes,
	}
	return resources
}

func (o relaydOptions) validate() error {
	if err := validateMultiaddrs(o.listenAddrs(), "peer listen address"); err != nil {
		return err
	}
	if err := validateMultiaddrs(o.advertiseAddrs(), "advertise address"); err != nil {
		return err
	}
	switch {
	case o.HTTPAddr == "":
		return fmt.Errorf("http address is required")
	case o.DBPath == "":
		return fmt.Errorf("db path is required")
	case o.IdentityKeyPath == "":
		return fmt.Errorf("identity key path is required")
	case len(o.listenAddrs()) == 0:
		return fmt.Errorf("at least one peer listen address is required")
	case o.TLSCertPath == "" && o.TLSKeyPath != "":
		return fmt.Errorf("tls cert path is required when tls key path is set")
	case o.TLSKeyPath == "" && o.TLSCertPath != "":
		return fmt.Errorf("tls key path is required when tls cert path is set")
	case o.ReadHeaderTimeout <= 0:
		return fmt.Errorf("read header timeout must be positive")
	case o.ReadTimeout <= 0:
		return fmt.Errorf("read timeout must be positive")
	case o.WriteTimeout <= 0:
		return fmt.Errorf("write timeout must be positive")
	case o.IdleTimeout <= 0:
		return fmt.Errorf("idle timeout must be positive")
	case o.ShutdownTimeout <= 0:
		return fmt.Errorf("shutdown timeout must be positive")
	case o.MaxBodyBytes <= 0:
		return fmt.Errorf("max body bytes must be positive")
	case o.RateLimitRequestsPerSecond < 0:
		return fmt.Errorf("rate limit rps must be non-negative")
	case o.RateLimitBurst < 0:
		return fmt.Errorf("rate limit burst must be non-negative")
	case o.RateLimitRequestsPerSecond > 0 && o.RateLimitBurst == 0:
		return fmt.Errorf("rate limit burst must be positive when rate limiting is enabled")
	case o.RateLimitIdleTTL <= 0:
		return fmt.Errorf("rate limit idle ttl must be positive")
	case o.ClientIPHeader != "" && len(o.TrustedProxies) == 0:
		return fmt.Errorf("trusted proxies are required when client ip header is set")
	case o.ReservationTTL <= 0:
		return fmt.Errorf("relay reservation ttl must be positive")
	case o.MaxReservations <= 0:
		return fmt.Errorf("relay max reservations must be positive")
	case o.MaxCircuits <= 0:
		return fmt.Errorf("relay max circuits must be positive")
	case o.MaxReservationsPerPeer <= 0:
		return fmt.Errorf("relay max reservations per peer must be positive")
	case o.MaxReservationsPerIP <= 0:
		return fmt.Errorf("relay max reservations per ip must be positive")
	case o.MaxReservationsPerASN <= 0:
		return fmt.Errorf("relay max reservations per asn must be positive")
	case o.RelayLimitDuration <= 0:
		return fmt.Errorf("relay limit duration must be positive")
	case o.RelayLimitDataBytes <= 0:
		return fmt.Errorf("relay limit data bytes must be positive")
	}
	if _, err := parseTrustedProxySet(o.TrustedProxies); err != nil {
		return err
	}
	return nil
}

func validateMultiaddrs(values []string, kind string) error {
	_, err := parseMultiaddrs(values, kind)
	return err
}

func parseMultiaddrs(values []string, kind string) ([]ma.Multiaddr, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]ma.Multiaddr, 0, len(values))
	for _, value := range compactNonEmptyStrings(values) {
		addr, err := ma.NewMultiaddr(value)
		if err != nil {
			return nil, fmt.Errorf("parse %s %q: %w", kind, value, err)
		}
		out = append(out, addr)
	}
	return out, nil
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func defaultStoragePath(filename string) string {
	if volumePath := strings.TrimSpace(os.Getenv(envRailwayVolume)); volumePath != "" {
		return filepath.Join(volumePath, filename)
	}
	return filename
}

func envListOrDefault(name string, fallback []string) []string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return compactNonEmptyStrings(strings.Split(value, ","))
	}
	if len(fallback) == 0 {
		return nil
	}
	out := make([]string, 0, len(fallback))
	out = append(out, fallback...)
	return out
}

func defaultHTTPAddrForEnv() string {
	if addr := strings.TrimSpace(os.Getenv(envHTTPAddr)); addr != "" {
		return addr
	}
	if port := strings.TrimSpace(os.Getenv(envPort)); port != "" {
		return ":" + port
	}
	return defaultHTTPAddr
}

func (o relaydOptions) TLSEnabled() bool {
	return o.TLSCertPath != "" && o.TLSKeyPath != ""
}

func serveHTTP(server *http.Server, opts relaydOptions) error {
	if opts.TLSEnabled() {
		return server.ListenAndServeTLS(opts.TLSCertPath, opts.TLSKeyPath)
	}
	return server.ListenAndServe()
}

func buildHTTPHandler(next http.Handler, opts relaydOptions, metrics *relaydMetrics) (http.Handler, *ipRateLimiter) {
	handler := http.Handler(next)
	handler = requestLogMiddleware(handler, metrics)
	handler = maxBodyBytesMiddleware(handler, opts.MaxBodyBytes)
	var limiter *ipRateLimiter
	if opts.RateLimitRequestsPerSecond > 0 {
		limiter = newIPRateLimiter(rate.Limit(opts.RateLimitRequestsPerSecond), opts.RateLimitBurst, opts.RateLimitIdleTTL)
		proxies, err := parseTrustedProxySet(opts.TrustedProxies)
		if err != nil {
			proxies = trustedProxySet{}
		}
		handler = rateLimitMiddleware(handler, limiter, proxies, opts.ClientIPHeader, metrics)
	}
	return handler, limiter
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			http.Error(w, fmt.Sprintf("request body exceeds %d bytes", maxBytesErr.Limit), http.StatusRequestEntityTooLarge)
			return false
		}
		http.Error(w, fmt.Sprintf("decode request: %v", err), http.StatusBadRequest)
		return false
	}
	return true
}

func maxBodyBytesMiddleware(next http.Handler, limit int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
	})
}

func rateLimitMiddleware(next http.Handler, limiter *ipRateLimiter, proxies trustedProxySet, clientIPHeader string, metrics *relaydMetrics) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if limiter == nil || r.URL.Path == "/healthz" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		if !limiter.Allow(clientIP(r, proxies, clientIPHeader)) {
			if metrics != nil {
				metrics.rateLimitRejectedTotal.Inc()
			}
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request, proxies trustedProxySet, clientIPHeader string) string {
	remote := remoteIP(r)
	if strings.TrimSpace(clientIPHeader) == "" || !proxies.Contains(remote) {
		return remote
	}
	switch strings.ToLower(strings.TrimSpace(clientIPHeader)) {
	case "x-forwarded-for":
		if ip := firstForwardedForIP(r.Header.Values(clientIPHeader)); ip != "" {
			return ip
		}
	default:
		if ip := parseHeaderIP(r.Header.Get(clientIPHeader)); ip != "" {
			return ip
		}
	}
	return remote
}

func remoteIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func firstForwardedForIP(values []string) string {
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if ip := parseHeaderIP(part); ip != "" {
				return ip
			}
		}
	}
	return ""
}

func parseHeaderIP(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.Contains(value, ":") {
		if host, _, err := net.SplitHostPort(value); err == nil {
			value = host
		}
	}
	ip := net.ParseIP(strings.Trim(value, "[]"))
	if ip == nil {
		return ""
	}
	return ip.String()
}

func parseTrustedProxySet(values []string) (trustedProxySet, error) {
	set := trustedProxySet{ips: make(map[string]struct{})}
	for _, value := range compactNonEmptyStrings(values) {
		if ip := net.ParseIP(value); ip != nil {
			set.ips[ip.String()] = struct{}{}
			continue
		}
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return trustedProxySet{}, fmt.Errorf("parse trusted proxy %q: %w", value, err)
		}
		set.nets = append(set.nets, network)
	}
	return set, nil
}

func (s trustedProxySet) Contains(value string) bool {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil {
		return false
	}
	if _, ok := s.ips[ip.String()]; ok {
		return true
	}
	for _, network := range s.nets {
		if network != nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

func newIPRateLimiter(limit rate.Limit, burst int, idleTTL time.Duration) *ipRateLimiter {
	return &ipRateLimiter{
		limit:    limit,
		burst:    burst,
		idleTTL:  idleTTL,
		visitors: make(map[string]*rateVisitor),
	}
}

func (l *ipRateLimiter) Allow(ip string) bool {
	if l == nil || strings.TrimSpace(ip) == "" {
		return true
	}
	now := time.Now().UTC()
	l.mu.Lock()
	defer l.mu.Unlock()
	visitor, ok := l.visitors[ip]
	if !ok {
		visitor = &rateVisitor{
			limiter:    rate.NewLimiter(l.limit, l.burst),
			lastSeenAt: now,
		}
		l.visitors[ip] = visitor
	}
	visitor.lastSeenAt = now
	return visitor.limiter.Allow()
}

func (l *ipRateLimiter) cleanupLoop(ctx context.Context) {
	if l == nil {
		return
	}
	ticker := time.NewTicker(minDuration(l.idleTTL/2, time.Minute))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.cleanup(time.Now().UTC())
		}
	}
}

func (l *ipRateLimiter) cleanup(now time.Time) {
	if l == nil {
		return
	}
	cutoff := now.Add(-l.idleTTL)
	l.mu.Lock()
	defer l.mu.Unlock()
	for ip, visitor := range l.visitors {
		if visitor == nil || visitor.lastSeenAt.Before(cutoff) {
			delete(l.visitors, ip)
		}
	}
}

func minDuration(left, right time.Duration) time.Duration {
	if left <= 0 || left > right {
		return right
	}
	return left
}

func configureSQLite(db *sql.DB) {
	if db == nil {
		return
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
}

func initSchema(db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	pragmas := []string{
		`PRAGMA journal_mode=WAL;`,
		`PRAGMA synchronous=NORMAL;`,
		`PRAGMA wal_autocheckpoint=1000;`,
		fmt.Sprintf(`PRAGMA busy_timeout=%d;`, sqliteBusyTimeout/time.Millisecond),
	}
	for _, query := range pragmas {
		if _, err := db.Exec(query); err != nil {
			return err
		}
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at INTEGER NOT NULL
	);`); err != nil {
		return err
	}
	for version := 1; version <= currentSchemaVersion; version++ {
		applied, err := schemaMigrationApplied(db, version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if err := applySchemaMigration(db, version); err != nil {
			return fmt.Errorf("apply schema migration %d: %w", version, err)
		}
	}
	return nil
}

func schemaMigrationApplied(db *sql.DB, version int) (bool, error) {
	var existing int
	err := db.QueryRow(`SELECT version FROM schema_migrations WHERE version = ?`, version).Scan(&existing)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return existing == version, nil
}

func applySchemaMigration(db *sql.DB, version int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, query := range schemaMigrationQueries(version) {
		if _, err := tx.Exec(query); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`, version, time.Now().UTC().Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

func schemaMigrationQueries(version int) []string {
	switch version {
	case 1:
		return []string{
			`CREATE TABLE IF NOT EXISTS presence_records (
				library_id TEXT NOT NULL,
				device_id TEXT NOT NULL,
				peer_id TEXT NOT NULL,
				addrs_json TEXT NOT NULL,
				expires_at INTEGER NOT NULL,
				updated_at INTEGER NOT NULL,
				PRIMARY KEY (library_id, device_id, peer_id)
			);`,
			`CREATE TABLE IF NOT EXISTS library_roots (
				library_id TEXT PRIMARY KEY,
				root_public_key TEXT NOT NULL,
				updated_at INTEGER NOT NULL
			);`,
			`CREATE TABLE IF NOT EXISTS member_auth_state (
				library_id TEXT NOT NULL,
				device_id TEXT NOT NULL,
				peer_id TEXT NOT NULL,
				cert_serial INTEGER NOT NULL,
				cert_expires_at INTEGER NOT NULL,
				updated_at INTEGER NOT NULL,
				PRIMARY KEY (library_id, device_id)
			);`,
			`CREATE INDEX IF NOT EXISTS idx_presence_peer ON presence_records(peer_id, updated_at DESC);`,
			`CREATE INDEX IF NOT EXISTS idx_presence_library_peer ON presence_records(library_id, peer_id, updated_at DESC);`,
		}
	case 2:
		return []string{
			`CREATE INDEX IF NOT EXISTS idx_presence_expires_at ON presence_records(expires_at);`,
			`CREATE INDEX IF NOT EXISTS idx_member_auth_peer ON member_auth_state(peer_id);`,
			`CREATE INDEX IF NOT EXISTS idx_member_auth_library_peer ON member_auth_state(library_id, peer_id);`,
			`CREATE INDEX IF NOT EXISTS idx_member_auth_cert_expires_at ON member_auth_state(cert_expires_at);`,
		}
	case 3:
		return []string{
			`CREATE TABLE IF NOT EXISTS revocation_state (
				library_id TEXT PRIMARY KEY,
				root_public_key TEXT NOT NULL,
				revision INTEGER NOT NULL,
				updated_at INTEGER NOT NULL
			);`,
			`CREATE TABLE IF NOT EXISTS revoked_invites (
				library_id TEXT NOT NULL,
				token_id TEXT NOT NULL,
				revision INTEGER NOT NULL,
				updated_at INTEGER NOT NULL,
				PRIMARY KEY (library_id, token_id)
			);`,
			`CREATE TABLE IF NOT EXISTS revoked_members (
				library_id TEXT NOT NULL,
				device_id TEXT NOT NULL,
				max_serial INTEGER NOT NULL,
				revision INTEGER NOT NULL,
				updated_at INTEGER NOT NULL,
				PRIMARY KEY (library_id, device_id)
			);`,
			`CREATE INDEX IF NOT EXISTS idx_revoked_invites_library_token ON revoked_invites(library_id, token_id);`,
			`CREATE INDEX IF NOT EXISTS idx_revoked_members_library_device ON revoked_members(library_id, device_id);`,
		}
	default:
		return nil
	}
}

func cleanupLoop(ctx context.Context, db *sql.DB, metrics *relaydMetrics) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanupExpiredRegistryState(db, time.Now().UTC(), metrics)
		}
	}
}

func cleanupExpiredRegistryState(db *sql.DB, now time.Time, metrics *relaydMetrics) {
	if db == nil {
		return
	}
	if _, err := db.Exec(`DELETE FROM presence_records WHERE expires_at < ?`, now.UTC().Unix()); err != nil {
		if metrics != nil {
			metrics.sqliteOperationFailures.WithLabelValues("cleanup_presence").Inc()
		}
		log.Printf("cleanup presence records: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM member_auth_state WHERE cert_expires_at > 0 AND cert_expires_at < ?`, now.UTC().UnixNano()); err != nil {
		if metrics != nil {
			metrics.sqliteOperationFailures.WithLabelValues("cleanup_member_auth").Inc()
		}
		log.Printf("cleanup member auth state: %v", err)
	}
}

func metricsLoop(ctx context.Context, db *sql.DB, metrics *relaydMetrics) {
	if db == nil || metrics == nil {
		return
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	collectRegistryMetrics(db, metrics)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			collectRegistryMetrics(db, metrics)
		}
	}
}

func collectRegistryMetrics(db *sql.DB, metrics *relaydMetrics) {
	now := time.Now().UTC().Unix()
	setCountGauge(db, metrics.presenceRecordsGauge, `SELECT COUNT(*) FROM presence_records WHERE expires_at >= ?`, now, metrics, "metrics_presence")
	setCountGauge(db, metrics.memberAuthStateGauge, `SELECT COUNT(*) FROM member_auth_state WHERE cert_expires_at <= 0 OR cert_expires_at >= ?`, time.Now().UTC().UnixNano(), metrics, "metrics_member_auth")
	setCountGauge(db, metrics.libraryRootsGauge, `SELECT COUNT(*) FROM library_roots`, nil, metrics, "metrics_library_roots")
}

func setCountGauge(db *sql.DB, gauge prometheus.Gauge, query string, arg any, metrics *relaydMetrics, op string) {
	if db == nil || gauge == nil {
		return
	}
	var (
		count int64
		err   error
	)
	if arg == nil {
		err = db.QueryRow(query).Scan(&count)
	} else {
		err = db.QueryRow(query, arg).Scan(&count)
	}
	if err != nil {
		if metrics != nil {
			metrics.sqliteOperationFailures.WithLabelValues(op).Inc()
		}
		return
	}
	gauge.Set(float64(count))
}

func (s *relaydServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	dbOK := false
	dbErr := ""
	if s != nil && s.db != nil {
		if err := s.db.Ping(); err != nil {
			dbErr = err.Error()
		} else {
			dbOK = true
		}
	}
	status := http.StatusOK
	if !dbOK {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{
		"peerId": hostIDString(s.host),
		"addrs":  formatHostAddrs(s.host),
		"db": map[string]any{
			"ok":    dbOK,
			"error": dbErr,
		},
		"time": time.Now().UTC(),
	})
}

func (s *relaydServer) handlePresenceAnnounce(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req registryauth.PresenceAnnounceRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	member, err := s.authenticateMembership(r.Context(), req.Record.LibraryID, req.RootPublicKey, req.Auth)
	if err != nil {
		s.metrics.registryEvent("presence_announce", "auth_failed")
		http.Error(w, fmt.Sprintf("authenticate presence announce: %v", err), http.StatusUnauthorized)
		return
	}
	record := req.Record
	record.LibraryID = strings.TrimSpace(record.LibraryID)
	record.DeviceID = strings.TrimSpace(record.DeviceID)
	record.PeerID = strings.TrimSpace(record.PeerID)
	if record.LibraryID == "" || record.DeviceID == "" || record.PeerID == "" {
		s.metrics.registryEvent("presence_announce", "bad_request")
		http.Error(w, "libraryId, deviceId, and peerId are required", http.StatusBadRequest)
		return
	}
	if record.LibraryID != member.LibraryID || record.DeviceID != member.DeviceID || record.PeerID != member.PeerID {
		s.metrics.registryEvent("presence_announce", "forbidden")
		http.Error(w, "presence record identity does not match authenticated membership", http.StatusForbidden)
		return
	}
	addrs, err := validatePresenceAddrs(record.Addrs, record.PeerID)
	if err != nil {
		s.metrics.registryEvent("presence_announce", "bad_address")
		http.Error(w, fmt.Sprintf("validate presence addrs: %v", err), http.StatusBadRequest)
		return
	}
	record.Addrs = addrs
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = time.Now().UTC()
	}
	if record.ExpiresAt.IsZero() || record.ExpiresAt.Before(record.UpdatedAt) || record.ExpiresAt.After(record.UpdatedAt.Add(90*time.Second)) {
		record.ExpiresAt = record.UpdatedAt.Add(90 * time.Second)
	}
	addrsJSON, err := json.Marshal(record.Addrs)
	if err != nil {
		http.Error(w, fmt.Sprintf("encode addrs: %v", err), http.StatusBadRequest)
		return
	}
	_, err = s.db.Exec(`
		INSERT INTO presence_records(library_id, device_id, peer_id, addrs_json, expires_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?)
		ON CONFLICT(library_id, device_id, peer_id) DO UPDATE SET
			addrs_json = excluded.addrs_json,
			expires_at = excluded.expires_at,
			updated_at = excluded.updated_at
	`, record.LibraryID, record.DeviceID, record.PeerID, string(addrsJSON), record.ExpiresAt.UTC().Unix(), record.UpdatedAt.UTC().Unix())
	if err != nil {
		s.metrics.registryEvent("presence_announce", "store_failed")
		http.Error(w, fmt.Sprintf("store presence: %v", err), http.StatusInternalServerError)
		return
	}
	s.metrics.registryEvent("presence_announce", "ok")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *relaydServer) handlePresenceMember(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req registryauth.MemberLookupRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	member, err := s.authenticateMembership(r.Context(), req.LibraryID, req.RootPublicKey, req.Auth)
	if err != nil {
		s.metrics.registryEvent("presence_member", "auth_failed")
		http.Error(w, fmt.Sprintf("authenticate member lookup: %v", err), http.StatusUnauthorized)
		return
	}
	if strings.TrimSpace(req.LibraryID) != member.LibraryID {
		s.metrics.registryEvent("presence_member", "forbidden")
		http.Error(w, "library lookup is not authorized", http.StatusForbidden)
		return
	}
	record, ok, err := s.lookupPresence(req.LibraryID, req.PeerID)
	if err != nil {
		s.metrics.registryEvent("presence_member", "lookup_failed")
		http.Error(w, fmt.Sprintf("lookup member: %v", err), http.StatusInternalServerError)
		return
	}
	if !ok {
		s.metrics.registryEvent("presence_member", "not_found")
		http.NotFound(w, r)
		return
	}
	s.metrics.registryEvent("presence_member", "ok")
	writeJSON(w, http.StatusOK, record)
}

func (s *relaydServer) handleRelayAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req registryauth.RelayAuthorizeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	member, err := s.authenticateMembership(r.Context(), req.LibraryID, req.RootPublicKey, req.Auth)
	if err != nil {
		s.metrics.registryEvent("relay_authorize", "auth_failed")
		http.Error(w, fmt.Sprintf("authenticate relay authorization: %v", err), http.StatusUnauthorized)
		return
	}
	s.metrics.registryEvent("relay_authorize", "ok")
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"peerId":  member.PeerID,
		"expires": member.CertExpiresAt,
	})
}

func (s *relaydServer) handleRevocationSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req registryauth.RevocationSyncRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := registryauth.VerifyRevocationSync(req); err != nil {
		s.metrics.registryEvent("revocation_sync", "auth_failed")
		http.Error(w, fmt.Sprintf("authenticate revocation sync: %v", err), http.StatusUnauthorized)
		return
	}
	if err := s.pinLibraryRoot(req.LibraryID, req.RootPublicKey); err != nil {
		s.metrics.registryEvent("revocation_sync", "pin_failed")
		http.Error(w, fmt.Sprintf("pin revocation library root: %v", err), http.StatusUnauthorized)
		return
	}
	applied, err := s.applyRevocationSync(r.Context(), req)
	if err != nil {
		s.metrics.registryEvent("revocation_sync", "store_failed")
		http.Error(w, fmt.Sprintf("store revocation sync: %v", err), http.StatusInternalServerError)
		return
	}
	result := "stale"
	if applied {
		result = "ok"
	}
	s.metrics.registryEvent("revocation_sync", result)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "applied": applied})
}

func (s *relaydServer) handleInviteOwner(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req registryauth.InviteOwnerLookupRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := registryauth.VerifyInviteAttestation(req.Invite, time.Now().UTC()); err != nil {
		s.metrics.registryEvent("invite_owner", "auth_failed")
		http.Error(w, fmt.Sprintf("authenticate invite lookup: %v", err), http.StatusUnauthorized)
		return
	}
	if err := s.pinLibraryRoot(req.Invite.LibraryID, req.Invite.RootPublicKey); err != nil {
		s.metrics.registryEvent("invite_owner", "pin_failed")
		http.Error(w, fmt.Sprintf("pin invite library root: %v", err), http.StatusUnauthorized)
		return
	}
	if revoked, err := s.inviteRevoked(req.Invite.LibraryID, req.Invite.TokenID); err != nil {
		s.metrics.registryEvent("invite_owner", "revocation_check_failed")
		http.Error(w, fmt.Sprintf("check invite revocation: %v", err), http.StatusInternalServerError)
		return
	} else if revoked {
		s.metrics.registryEvent("invite_owner", "revoked")
		http.Error(w, "invite is revoked", http.StatusUnauthorized)
		return
	}
	record, ok, err := s.lookupPresence(req.Invite.LibraryID, req.Invite.OwnerPeerID)
	if err != nil {
		s.metrics.registryEvent("invite_owner", "lookup_failed")
		http.Error(w, fmt.Sprintf("lookup owner: %v", err), http.StatusInternalServerError)
		return
	}
	if !ok {
		s.metrics.registryEvent("invite_owner", "not_found")
		http.NotFound(w, r)
		return
	}
	record.Addrs = inviteLookupRelayAddrsForPeer(record.Addrs, req.Invite.OwnerPeerID)
	if len(record.Addrs) == 0 {
		s.metrics.registryEvent("invite_owner", "not_found")
		http.NotFound(w, r)
		return
	}
	s.metrics.registryEvent("invite_owner", "ok")
	writeJSON(w, http.StatusOK, record)
}

func inviteLookupRelayAddrs(addrs []string) []string {
	return inviteLookupRelayAddrsForPeer(addrs, "")
}

func inviteLookupRelayAddrsForPeer(addrs []string, peerID string) []string {
	valid, err := validatePresenceAddrs(addrs, peerID)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(valid))
	for _, addr := range valid {
		parsed, err := ma.NewMultiaddr(addr)
		if err != nil {
			continue
		}
		if !multiaddrHasProtocol(parsed, ma.P_CIRCUIT) {
			continue
		}
		out = append(out, addr)
	}
	return compactNonEmptyStrings(out)
}

func validatePresenceAddrs(addrs []string, peerID string) ([]string, error) {
	addrs = compactNonEmptyStrings(addrs)
	if len(addrs) > maxPresenceAddrs {
		return nil, fmt.Errorf("too many addresses: %d > %d", len(addrs), maxPresenceAddrs)
	}
	totalBytes := 0
	out := make([]string, 0, len(addrs))
	for _, value := range addrs {
		totalBytes += len(value)
		if totalBytes > maxPresenceAddrsBytes {
			return nil, fmt.Errorf("addresses exceed %d bytes", maxPresenceAddrsBytes)
		}
		addr, err := ma.NewMultiaddr(value)
		if err != nil {
			return nil, fmt.Errorf("parse address %q: %w", value, err)
		}
		if expectedPeerID := strings.TrimSpace(peerID); expectedPeerID != "" {
			actualPeerID := finalMultiaddrPeerID(addr)
			if actualPeerID == "" {
				return nil, fmt.Errorf("address %q is missing final peer id", value)
			}
			if actualPeerID != expectedPeerID {
				return nil, fmt.Errorf("address %q peer id mismatch", value)
			}
		}
		out = append(out, addr.String())
	}
	return compactNonEmptyStrings(out), nil
}

func multiaddrHasProtocol(addr ma.Multiaddr, protocolCode int) bool {
	if addr == nil {
		return false
	}
	for _, component := range addr {
		if component.Code() == protocolCode {
			return true
		}
	}
	return false
}

func finalMultiaddrPeerID(addr ma.Multiaddr) string {
	if addr == nil {
		return ""
	}
	var value string
	for _, component := range addr {
		if component.Code() == ma.P_P2P {
			value = strings.TrimSpace(component.Value())
		}
	}
	return value
}

func (s *relaydServer) lookupPresence(libraryID, peerID string) (presenceRecord, bool, error) {
	if strings.TrimSpace(peerID) == "" {
		return presenceRecord{}, false, nil
	}
	var (
		query = `SELECT library_id, device_id, peer_id, addrs_json, expires_at, updated_at
			FROM presence_records
			WHERE peer_id = ? AND expires_at >= ?`
		args []any
	)
	args = append(args, strings.TrimSpace(peerID), time.Now().UTC().Unix())
	if strings.TrimSpace(libraryID) != "" {
		query += ` AND library_id = ?`
		args = append(args, strings.TrimSpace(libraryID))
	}
	query += ` ORDER BY updated_at DESC LIMIT 1`

	var (
		record    presenceRecord
		addrsJSON string
		expiresAt int64
		updatedAt int64
	)
	err := s.db.QueryRow(query, args...).Scan(&record.LibraryID, &record.DeviceID, &record.PeerID, &addrsJSON, &expiresAt, &updatedAt)
	if err == sql.ErrNoRows {
		return presenceRecord{}, false, nil
	}
	if err != nil {
		return presenceRecord{}, false, err
	}
	if err := json.Unmarshal([]byte(addrsJSON), &record.Addrs); err != nil {
		return presenceRecord{}, false, err
	}
	record.Addrs = compactNonEmptyStrings(record.Addrs)
	record.ExpiresAt = time.Unix(expiresAt, 0).UTC()
	record.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	return record, true, nil
}

func (s *relaydServer) applyRevocationSync(ctx context.Context, req registryauth.RevocationSyncRequest) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var existingRevision int64
	err = tx.QueryRow(`SELECT revision FROM revocation_state WHERE library_id = ?`, strings.TrimSpace(req.LibraryID)).Scan(&existingRevision)
	switch {
	case err == nil && existingRevision >= req.Revision:
		return false, tx.Commit()
	case err == sql.ErrNoRows:
	case err != nil:
		return false, err
	}
	now := time.Now().UTC().Unix()
	if _, err := tx.Exec(`
		INSERT INTO revocation_state(library_id, root_public_key, revision, updated_at)
		VALUES(?, ?, ?, ?)
		ON CONFLICT(library_id) DO UPDATE SET
			root_public_key = excluded.root_public_key,
			revision = excluded.revision,
			updated_at = excluded.updated_at
	`, strings.TrimSpace(req.LibraryID), strings.TrimSpace(req.RootPublicKey), req.Revision, now); err != nil {
		return false, err
	}
	for _, tokenID := range compactNonEmptyStrings(req.InviteTokenIDs) {
		if _, err := tx.Exec(`
			INSERT INTO revoked_invites(library_id, token_id, revision, updated_at)
			VALUES(?, ?, ?, ?)
			ON CONFLICT(library_id, token_id) DO UPDATE SET
				revision = max(revoked_invites.revision, excluded.revision),
				updated_at = excluded.updated_at
		`, strings.TrimSpace(req.LibraryID), tokenID, req.Revision, now); err != nil {
			return false, err
		}
	}
	for _, revocation := range req.MembershipRevocations {
		deviceID := strings.TrimSpace(revocation.DeviceID)
		if deviceID == "" || revocation.MaxSerial <= 0 {
			continue
		}
		if _, err := tx.Exec(`
			INSERT INTO revoked_members(library_id, device_id, max_serial, revision, updated_at)
			VALUES(?, ?, ?, ?, ?)
			ON CONFLICT(library_id, device_id) DO UPDATE SET
				max_serial = max(revoked_members.max_serial, excluded.max_serial),
				revision = max(revoked_members.revision, excluded.revision),
				updated_at = excluded.updated_at
		`, strings.TrimSpace(req.LibraryID), deviceID, revocation.MaxSerial, req.Revision, now); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *relaydServer) inviteRevoked(libraryID, tokenID string) (bool, error) {
	var exists int
	err := s.db.QueryRow(`SELECT 1 FROM revoked_invites WHERE library_id = ? AND token_id = ? LIMIT 1`, strings.TrimSpace(libraryID), strings.TrimSpace(tokenID)).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil && exists == 1, err
}

func (s *relaydServer) membershipRevoked(libraryID, deviceID string, serial int64) (bool, error) {
	return membershipRevokedQuery(s.db, libraryID, deviceID, serial)
}

func membershipRevokedQuery(db sqlQueryExecutor, libraryID, deviceID string, serial int64) (bool, error) {
	var maxSerial int64
	err := db.QueryRow(`SELECT max_serial FROM revoked_members WHERE library_id = ? AND device_id = ?`, strings.TrimSpace(libraryID), strings.TrimSpace(deviceID)).Scan(&maxSerial)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return serial <= maxSerial, nil
}

type sqlQueryExecutor interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
}

func (s *relaydServer) authenticateMembership(ctx context.Context, libraryID, claimedRootPublicKey string, auth registryauth.TransportPeerAuth) (verifiedMembership, error) {
	libraryID = strings.TrimSpace(libraryID)
	claimedRootPublicKey = strings.TrimSpace(claimedRootPublicKey)
	cert := auth.Cert
	cert.LibraryID = strings.TrimSpace(cert.LibraryID)
	cert.DeviceID = strings.TrimSpace(cert.DeviceID)
	cert.PeerID = strings.TrimSpace(cert.PeerID)
	if libraryID == "" || cert.DeviceID == "" || cert.PeerID == "" {
		return verifiedMembership{}, fmt.Errorf("library id, device id, and peer id are required")
	}
	if cert.LibraryID != libraryID {
		return verifiedMembership{}, fmt.Errorf("membership certificate library mismatch")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return verifiedMembership{}, err
	}
	defer tx.Rollback()
	member, err := authenticateMembershipTx(tx, libraryID, claimedRootPublicKey, auth)
	if err != nil {
		return verifiedMembership{}, err
	}
	if err := tx.Commit(); err != nil {
		return verifiedMembership{}, err
	}
	return member, nil
}

func authenticateMembershipTx(db sqlQueryExecutor, libraryID, claimedRootPublicKey string, auth registryauth.TransportPeerAuth) (verifiedMembership, error) {
	libraryID = strings.TrimSpace(libraryID)
	claimedRootPublicKey = strings.TrimSpace(claimedRootPublicKey)
	cert := auth.Cert
	cert.LibraryID = strings.TrimSpace(cert.LibraryID)
	cert.DeviceID = strings.TrimSpace(cert.DeviceID)
	cert.PeerID = strings.TrimSpace(cert.PeerID)
	pinnedRootPublicKey, ok, err := libraryRootPublicKeyQuery(db, libraryID)
	if err != nil {
		return verifiedMembership{}, err
	}
	rootPublicKey := claimedRootPublicKey
	if ok {
		if rootPublicKey == "" {
			rootPublicKey = pinnedRootPublicKey
		}
		if rootPublicKey != pinnedRootPublicKey {
			return verifiedMembership{}, fmt.Errorf("library root public key mismatch")
		}
	} else if rootPublicKey == "" {
		return verifiedMembership{}, fmt.Errorf("root public key is required")
	}
	if err := registryauth.VerifyMembershipCert(cert, auth.AuthorityChain, rootPublicKey, time.Now().UTC(), libraryID, cert.DeviceID, cert.PeerID); err != nil {
		return verifiedMembership{}, err
	}
	if revoked, err := membershipRevokedQuery(db, libraryID, cert.DeviceID, cert.Serial); err != nil {
		return verifiedMembership{}, err
	} else if revoked {
		return verifiedMembership{}, fmt.Errorf("membership certificate is revoked")
	}
	member := verifiedMembership{
		LibraryID:     libraryID,
		DeviceID:      cert.DeviceID,
		PeerID:        cert.PeerID,
		RootPublicKey: rootPublicKey,
		CertSerial:    cert.Serial,
		CertExpiresAt: cert.ExpiresAt,
	}
	if err := pinLibraryRootQuery(db, libraryID, rootPublicKey); err != nil {
		return verifiedMembership{}, err
	}
	if err := persistMemberAuthStateQuery(db, member); err != nil {
		return verifiedMembership{}, err
	}
	return member, nil
}

func (s *relaydServer) libraryRootPublicKey(libraryID string) (string, bool, error) {
	return libraryRootPublicKeyQuery(s.db, libraryID)
}

func libraryRootPublicKeyQuery(db sqlQueryExecutor, libraryID string) (string, bool, error) {
	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" {
		return "", false, nil
	}
	var rootPublicKey string
	err := db.QueryRow(`SELECT root_public_key FROM library_roots WHERE library_id = ?`, libraryID).Scan(&rootPublicKey)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(rootPublicKey), true, nil
}

func (s *relaydServer) pinLibraryRoot(libraryID, rootPublicKey string) error {
	return pinLibraryRootQuery(s.db, libraryID, rootPublicKey)
}

func pinLibraryRootQuery(db sqlQueryExecutor, libraryID, rootPublicKey string) error {
	libraryID = strings.TrimSpace(libraryID)
	rootPublicKey = strings.TrimSpace(rootPublicKey)
	if libraryID == "" || rootPublicKey == "" {
		return fmt.Errorf("library id and root public key are required")
	}
	existing, ok, err := libraryRootPublicKeyQuery(db, libraryID)
	if err != nil {
		return err
	}
	if ok {
		if existing != rootPublicKey {
			return fmt.Errorf("library root public key mismatch")
		}
		_, err = db.Exec(`UPDATE library_roots SET updated_at = ? WHERE library_id = ?`, time.Now().UTC().Unix(), libraryID)
		return err
	}
	_, err = db.Exec(`
		INSERT INTO library_roots(library_id, root_public_key, updated_at)
		VALUES(?, ?, ?)
	`, libraryID, rootPublicKey, time.Now().UTC().Unix())
	return err
}

func (s *relaydServer) persistMemberAuthState(member verifiedMembership) error {
	return persistMemberAuthStateQuery(s.db, member)
}

func persistMemberAuthStateQuery(db sqlQueryExecutor, member verifiedMembership) error {
	var existingSerial int64
	var existingPeerID string
	err := db.QueryRow(`
		SELECT cert_serial, peer_id
		FROM member_auth_state
		WHERE library_id = ? AND device_id = ?
	`, member.LibraryID, member.DeviceID).Scan(&existingSerial, &existingPeerID)
	switch {
	case err == nil:
		if existingSerial > member.CertSerial {
			return fmt.Errorf("membership certificate serial is stale")
		}
		if existingSerial == member.CertSerial && strings.TrimSpace(existingPeerID) != "" && strings.TrimSpace(existingPeerID) != member.PeerID {
			return fmt.Errorf("membership certificate peer mismatch")
		}
	case err == sql.ErrNoRows:
	default:
		return err
	}
	_, err = db.Exec(`
		INSERT INTO member_auth_state(library_id, device_id, peer_id, cert_serial, cert_expires_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?)
		ON CONFLICT(library_id, device_id) DO UPDATE SET
			peer_id = excluded.peer_id,
			cert_serial = excluded.cert_serial,
			cert_expires_at = excluded.cert_expires_at,
			updated_at = excluded.updated_at
	`, member.LibraryID, member.DeviceID, member.PeerID, member.CertSerial, member.CertExpiresAt, time.Now().UTC().Unix())
	return err
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func newRelaydMetrics(registerer prometheus.Registerer) *relaydMetrics {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}
	metrics := &relaydMetrics{
		httpRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "relayd_http_requests_total",
			Help: "Total relayd HTTP requests.",
		}, []string{"method", "path", "status"}),
		httpRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "relayd_http_request_duration_seconds",
			Help:    "Relayd HTTP request duration.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "path", "status"}),
		registryEventsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "relayd_registry_events_total",
			Help: "Registry events by operation and result.",
		}, []string{"operation", "result"}),
		rateLimitRejectedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "relayd_rate_limit_rejected_total",
			Help: "HTTP requests rejected by the per-client rate limiter.",
		}),
		presenceRecordsGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "relayd_presence_records",
			Help: "Currently active presence records.",
		}),
		memberAuthStateGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "relayd_member_auth_state_records",
			Help: "Currently valid member auth state records.",
		}),
		libraryRootsGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "relayd_library_roots",
			Help: "Pinned library root records.",
		}),
		sqliteOperationFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "relayd_sqlite_operation_failures_total",
			Help: "SQLite operation failures by operation.",
		}, []string{"operation"}),
		relayACLDecisionsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "relayd_relay_acl_decisions_total",
			Help: "Circuit relay ACL decisions by operation and result.",
		}, []string{"operation", "result"}),
	}
	registerer.MustRegister(
		metrics.httpRequestsTotal,
		metrics.httpRequestDuration,
		metrics.registryEventsTotal,
		metrics.rateLimitRejectedTotal,
		metrics.presenceRecordsGauge,
		metrics.memberAuthStateGauge,
		metrics.libraryRootsGauge,
		metrics.sqliteOperationFailures,
		metrics.relayACLDecisionsTotal,
	)
	return metrics
}

func (m *relaydMetrics) registryEvent(operation, result string) {
	if m == nil {
		return
	}
	m.registryEventsTotal.WithLabelValues(operation, result).Inc()
}

func requestLogMiddleware(next http.Handler, metrics *relaydMetrics) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		recorder := &statusResponseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		duration := time.Since(startedAt)
		status := fmt.Sprintf("%d", recorder.status)
		if metrics != nil {
			metrics.httpRequestsTotal.WithLabelValues(r.Method, r.URL.Path, status).Inc()
			metrics.httpRequestDuration.WithLabelValues(r.Method, r.URL.Path, status).Observe(duration.Seconds())
		}
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, recorder.status, duration.Round(time.Millisecond))
	})
}

func (w *statusResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func hostIDString(h host.Host) string {
	if h == nil {
		return ""
	}
	return h.ID().String()
}

func formatHostAddrs(h host.Host) []string {
	if h == nil {
		return nil
	}
	out := make([]string, 0, len(h.Addrs()))
	for _, addr := range h.Addrs() {
		out = append(out, fmt.Sprintf("%s/p2p/%s", addr.String(), h.ID().String()))
	}
	return compactNonEmptyStrings(out)
}

func compactNonEmptyStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
