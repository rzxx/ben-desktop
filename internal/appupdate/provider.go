package appupdate

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

const defaultGitHubAPI = "https://api.github.com"

type SignedGitHubConfig struct {
	Repository       string
	Token            string
	BaseURL          string
	Prerelease       bool
	ChecksumAsset    string
	SignatureAsset   string
	RequireSignature bool
	HTTPClient       *http.Client
}

type SignedGitHubProvider struct {
	repository       string
	token            string
	baseURL          string
	signatureAsset   string
	requireSignature bool
	client           *http.Client
	inner            updater.Provider
}

func NewSignedGitHubProvider(cfg SignedGitHubConfig) (*SignedGitHubProvider, error) {
	repository := strings.TrimSpace(cfg.Repository)
	if repository == "" {
		return nil, errors.New("appupdate: GitHub repository is required")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultGitHubAPI
	}
	inner, err := github.New(github.Config{
		Repository:    repository,
		Token:         cfg.Token,
		Prerelease:    cfg.Prerelease,
		BaseURL:       baseURL,
		ChecksumAsset: cfg.ChecksumAsset,
		HTTPClient:    client,
	})
	if err != nil {
		return nil, err
	}
	signatureAsset := strings.TrimSpace(cfg.SignatureAsset)
	if signatureAsset == "" {
		signatureAsset = ".sig"
	}
	return &SignedGitHubProvider{
		repository:       repository,
		token:            cfg.Token,
		baseURL:          baseURL,
		signatureAsset:   signatureAsset,
		requireSignature: cfg.RequireSignature,
		client:           client,
		inner:            inner,
	}, nil
}

func (p *SignedGitHubProvider) Name() string {
	return p.inner.Name()
}

func (p *SignedGitHubProvider) Check(ctx context.Context, req updater.CheckRequest) (*updater.Release, error) {
	rel, err := p.inner.Check(ctx, req)
	if err != nil || rel == nil {
		return rel, err
	}

	signature, err := p.fetchSignature(ctx, rel)
	if err != nil {
		return nil, err
	}
	if len(signature) == 0 {
		if p.requireSignature {
			return nil, fmt.Errorf("appupdate: release %s asset %s is missing signature sidecar", rel.Version, rel.Artifact.Filename)
		}
		return rel, nil
	}
	if rel.Verification == nil {
		rel.Verification = &updater.Verification{}
	}
	rel.Verification.SignatureAlgo = "ed25519"
	rel.Verification.Signature = signature
	return rel, nil
}

func (p *SignedGitHubProvider) Download(ctx context.Context, rel *updater.Release, dst io.Writer, onProgress func(written, total int64)) error {
	return p.inner.Download(ctx, rel, dst, onProgress)
}

func (p *SignedGitHubProvider) fetchSignature(ctx context.Context, rel *updater.Release) ([]byte, error) {
	tag := releaseTag(rel)
	if tag == "" || rel.Artifact.Filename == "" {
		return nil, nil
	}
	endpoint := p.baseURL + "/repos/" + p.repository + "/releases/tags/" + url.PathEscape(tag)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	p.setAuth(req)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("appupdate: fetch release signatures: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("appupdate: fetch release signatures: HTTP %d", resp.StatusCode)
	}
	var apiRel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&apiRel); err != nil {
		return nil, fmt.Errorf("appupdate: decode release signatures: %w", err)
	}

	signatureName := rel.Artifact.Filename + p.signatureAsset
	for _, asset := range apiRel.Assets {
		if asset.Name != signatureName {
			continue
		}
		return p.downloadSignature(ctx, asset.BrowserDownloadURL, signatureName)
	}
	return nil, nil
}

func (p *SignedGitHubProvider) downloadSignature(ctx context.Context, assetURL string, assetName string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/octet-stream")
	p.setAuth(req)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("appupdate: download %s: %w", assetName, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("appupdate: download %s: HTTP %d", assetName, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return nil, err
	}
	signature, err := decodeSignature(body)
	if err != nil {
		return nil, fmt.Errorf("appupdate: decode %s: %w", assetName, err)
	}
	return signature, nil
}

func (p *SignedGitHubProvider) setAuth(req *http.Request) {
	if strings.TrimSpace(p.token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(p.token))
	}
}

func releaseTag(rel *updater.Release) string {
	if rel == nil {
		return ""
	}
	if rel.Metadata != nil {
		if tag, ok := rel.Metadata["github.release.tag"].(string); ok && strings.TrimSpace(tag) != "" {
			return strings.TrimSpace(tag)
		}
	}
	version := strings.TrimSpace(rel.Version)
	if version == "" {
		return ""
	}
	if strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}

func decodeSignature(body []byte) ([]byte, error) {
	raw := strings.TrimSpace(string(body))
	if raw == "" {
		return nil, errors.New("empty signature")
	}
	if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(raw); err == nil {
		return decoded, nil
	}
	if decoded, err := hex.DecodeString(raw); err == nil {
		return decoded, nil
	}
	if len(body) == 64 {
		return body, nil
	}
	return nil, errors.New("signature is not base64, hex, or raw Ed25519 bytes")
}

func DecodeSignature(body []byte) ([]byte, error) {
	return decodeSignature(body)
}

type githubRelease struct {
	Assets []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}
