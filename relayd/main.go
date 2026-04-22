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
	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
	relayv2 "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	ma "github.com/multiformats/go-multiaddr"
	"golang.org/x/time/rate"
	_ "modernc.org/sqlite"
)

const (
	defaultHTTPAddr        = ":8787"
	defaultDBPath          = "ben-relayd.db"
	defaultIdentityKeyPath = "ben-relayd.identity.key"
	defaultMaxBodyBytes    = int64(1 << 20)
	sqliteBusyTimeout      = 5 * time.Second
)

const (
	envPort            = "PORT"
	envHTTPAddr        = "RELAYD_HTTP_ADDR"
	envDBPath          = "RELAYD_DB_PATH"
	envIdentityKeyPath = "RELAYD_IDENTITY_KEY_PATH"
	envPeerListenAddrs = "RELAYD_PEER_LISTEN_ADDRS"
	envAdvertiseAddrs  = "RELAYD_ADVERTISE_ADDRS"
	envTLSCertPath     = "RELAYD_TLS_CERT_PATH"
	envTLSKeyPath      = "RELAYD_TLS_KEY_PATH"
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
	db   *sql.DB
	host host.Host
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

	db, err := sql.Open("sqlite", opts.DBPath)
	if err != nil {
		log.Fatalf("open registry db: %v", err)
	}
	defer db.Close()
	configureSQLite(db)
	if err := initSchema(db); err != nil {
		log.Fatalf("init registry db: %v", err)
	}

	hostNode, err := newRelayHost(opts)
	if err != nil {
		log.Fatalf("create relay host: %v", err)
	}
	defer hostNode.Close()

	server := &relaydServer{db: db, host: hostNode}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", server.handleHealth)
	mux.HandleFunc("/v1/presence/announce", server.handlePresenceAnnounce)
	mux.HandleFunc("/v1/presence/member", server.handlePresenceMember)
	mux.HandleFunc("/v1/invites/owner", server.handleInviteOwner)

	handler, limiter := buildHTTPHandler(mux, opts)
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

	go cleanupLoop(ctx, db)
	if limiter != nil {
		go limiter.cleanupLoop(ctx)
	}

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
	fs.StringVar(&opts.HTTPAddr, "http-addr", defaultHTTPAddrForEnv(), "HTTP listen address")
	fs.StringVar(&opts.DBPath, "db", envOrDefault(envDBPath, defaultDBPath), "SQLite registry database path")
	fs.StringVar(&opts.IdentityKeyPath, "identity-key", envOrDefault(envIdentityKeyPath, defaultIdentityKeyPath), "libp2p relay identity private key path")
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
	if err := opts.validate(); err != nil {
		return relaydOptions{}, err
	}
	return opts, nil
}

func newRelayHost(opts relaydOptions) (host.Host, error) {
	priv, err := loadOrCreateRelayIdentityKey(opts.IdentityKeyPath)
	if err != nil {
		return nil, fmt.Errorf("load relay identity: %w", err)
	}
	rm, err := newRelayResourceManager()
	if err != nil {
		return nil, err
	}
	libp2pOpts := []libp2p.Option{
		libp2p.Identity(priv),
		libp2p.ListenAddrStrings(opts.listenAddrs()...),
		libp2p.ResourceManager(rm),
		libp2p.ForceReachabilityPublic(),
		libp2p.EnableRelayService(relayv2.WithResources(opts.relayResources())),
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
	default:
		return nil
	}
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

func buildHTTPHandler(next http.Handler, opts relaydOptions) (http.Handler, *ipRateLimiter) {
	handler := http.Handler(next)
	handler = requestLogMiddleware(handler)
	handler = maxBodyBytesMiddleware(handler, opts.MaxBodyBytes)
	var limiter *ipRateLimiter
	if opts.RateLimitRequestsPerSecond > 0 {
		limiter = newIPRateLimiter(rate.Limit(opts.RateLimitRequestsPerSecond), opts.RateLimitBurst, opts.RateLimitIdleTTL)
		handler = rateLimitMiddleware(handler, limiter)
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

func rateLimitMiddleware(next http.Handler, limiter *ipRateLimiter) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if limiter == nil || r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		if !limiter.Allow(remoteIP(r)) {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
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
	queries := []string{
		`PRAGMA journal_mode=WAL;`,
		fmt.Sprintf(`PRAGMA busy_timeout=%d;`, sqliteBusyTimeout/time.Millisecond),
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
	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return err
		}
	}
	return nil
}

func cleanupLoop(ctx context.Context, db *sql.DB) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := db.Exec(`DELETE FROM presence_records WHERE expires_at < ?`, time.Now().UTC().Unix()); err != nil {
				log.Printf("cleanup presence records: %v", err)
			}
		}
	}
}

func (s *relaydServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"peerId": hostIDString(s.host),
		"addrs":  formatHostAddrs(s.host),
		"time":   time.Now().UTC(),
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
		http.Error(w, fmt.Sprintf("authenticate presence announce: %v", err), http.StatusUnauthorized)
		return
	}
	record := req.Record
	record.LibraryID = strings.TrimSpace(record.LibraryID)
	record.DeviceID = strings.TrimSpace(record.DeviceID)
	record.PeerID = strings.TrimSpace(record.PeerID)
	record.Addrs = compactNonEmptyStrings(record.Addrs)
	if record.LibraryID == "" || record.DeviceID == "" || record.PeerID == "" {
		http.Error(w, "libraryId, deviceId, and peerId are required", http.StatusBadRequest)
		return
	}
	if record.LibraryID != member.LibraryID || record.DeviceID != member.DeviceID || record.PeerID != member.PeerID {
		http.Error(w, "presence record identity does not match authenticated membership", http.StatusForbidden)
		return
	}
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
		http.Error(w, fmt.Sprintf("store presence: %v", err), http.StatusInternalServerError)
		return
	}
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
		http.Error(w, fmt.Sprintf("authenticate member lookup: %v", err), http.StatusUnauthorized)
		return
	}
	if strings.TrimSpace(req.LibraryID) != member.LibraryID {
		http.Error(w, "library lookup is not authorized", http.StatusForbidden)
		return
	}
	record, ok, err := s.lookupPresence(req.LibraryID, req.PeerID)
	if err != nil {
		http.Error(w, fmt.Sprintf("lookup member: %v", err), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, record)
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
		http.Error(w, fmt.Sprintf("authenticate invite lookup: %v", err), http.StatusUnauthorized)
		return
	}
	if err := s.pinLibraryRoot(req.Invite.LibraryID, req.Invite.RootPublicKey); err != nil {
		http.Error(w, fmt.Sprintf("pin invite library root: %v", err), http.StatusUnauthorized)
		return
	}
	record, ok, err := s.lookupPresence(req.Invite.LibraryID, req.Invite.OwnerPeerID)
	if err != nil {
		http.Error(w, fmt.Sprintf("lookup owner: %v", err), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	record.Addrs = inviteLookupRelayAddrs(record.Addrs)
	if len(record.Addrs) == 0 {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func inviteLookupRelayAddrs(addrs []string) []string {
	out := make([]string, 0, len(addrs))
	for _, addr := range compactNonEmptyStrings(addrs) {
		if !strings.Contains(addr, "/p2p-circuit") {
			continue
		}
		out = append(out, addr)
	}
	return compactNonEmptyStrings(out)
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

func requestLogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(startedAt).Round(time.Millisecond))
	})
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
