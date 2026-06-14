package main

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type runtimeManifest struct {
	SchemaVersion  int                   `json:"schemaVersion"`
	Version        string                `json:"version"`
	RuntimeVersion string                `json:"runtimeVersion,omitempty"`
	Platform       string                `json:"platform"`
	Arch           string                `json:"arch"`
	Asset          string                `json:"asset"`
	AssetSHA256    string                `json:"assetSha256"`
	Files          []runtimeManifestFile `json:"files"`
	GeneratedAt    string                `json:"generatedAt"`
}

type runtimeManifestFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type bundleEntry struct {
	target string
	source string
	data   []byte
}

func main() {
	if len(os.Args) < 2 {
		fail("usage: benreleasetool <keygen|runtime-bundle|sign> [flags]")
	}
	var err error
	switch os.Args[1] {
	case "keygen":
		err = runKeygen()
	case "runtime-bundle":
		err = runRuntimeBundle(os.Args[2:])
	case "sign":
		err = runSign(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fail(err.Error())
	}
}

func runKeygen() error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return err
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return err
	}
	fmt.Print(string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})))
	fmt.Print(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})))
	return nil
}

func runRuntimeBundle(args []string) error {
	fs := flag.NewFlagSet("runtime-bundle", flag.ExitOnError)
	source := fs.String("source", "build/windows/runtime", "Windows runtime staging directory")
	outDir := fs.String("out-dir", "dist", "output directory")
	versionFile := fs.String("version-file", "build/windows/runtime/version.txt", "runtime version file")
	asset := fs.String("asset", "ben-desktop-runtime-windows-amd64.zip", "runtime zip asset name")
	require := fs.Bool("require", false, "fail when required runtime files are missing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	versionBody, err := os.ReadFile(*versionFile)
	if err != nil {
		return err
	}
	version := strings.TrimSpace(string(versionBody))
	if version == "" {
		return errors.New("runtime version is empty")
	}
	entries, err := collectRuntimeEntries(*source, version, *require)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return errors.New("runtime bundle has no files")
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}
	zipPath := filepath.Join(*outDir, *asset)
	files, err := writeRuntimeZip(zipPath, entries)
	if err != nil {
		return err
	}
	zipSHA, err := fileSHA256(zipPath)
	if err != nil {
		return err
	}
	manifest := runtimeManifest{
		SchemaVersion:  1,
		Version:        version,
		RuntimeVersion: version,
		Platform:       "windows",
		Arch:           "amd64",
		Asset:          *asset,
		AssetSHA256:    zipSHA,
		Files:          files,
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	manifestBody, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*outDir, "runtime-manifest.json"), append(manifestBody, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("%s\n%s\n", zipPath, filepath.Join(*outDir, "runtime-manifest.json"))
	return nil
}

func runSign(args []string) error {
	fs := flag.NewFlagSet("sign", flag.ExitOnError)
	keyEnv := fs.String("key-env", "UPDATER_PRIVATE_KEY_PEM", "environment variable containing an Ed25519 private key PEM")
	keyFile := fs.String("key-file", "", "file containing an Ed25519 private key PEM")
	sumsPath := fs.String("sums", "", "optional SHA256SUMS output path")
	signSums := fs.Bool("sign-sums", true, "also sign the SHA256SUMS file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	files := fs.Args()
	if len(files) == 0 {
		return errors.New("no files to sign")
	}
	keyPEM, err := loadPrivateKeyPEM(*keyEnv, *keyFile)
	if err != nil {
		return err
	}
	privateKey, err := parsePrivateKey(keyPEM)
	if err != nil {
		return err
	}
	sumLines := make([]string, 0, len(files))
	for _, file := range files {
		sum, err := signFile(privateKey, file)
		if err != nil {
			return err
		}
		sumLines = append(sumLines, fmt.Sprintf("%s  %s", sum, filepath.Base(file)))
	}
	if *sumsPath != "" {
		sort.Strings(sumLines)
		if err := os.WriteFile(*sumsPath, []byte(strings.Join(sumLines, "\n")+"\n"), 0o644); err != nil {
			return err
		}
		if *signSums {
			if _, err := signFile(privateKey, *sumsPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func collectRuntimeEntries(source string, version string, require bool) ([]bundleEntry, error) {
	entries := map[string]bundleEntry{}
	addFile := func(sourcePath string, target string) error {
		if _, err := os.Stat(sourcePath); err != nil {
			if require {
				return err
			}
			return nil
		}
		target = filepath.ToSlash(target)
		if _, exists := entries[target]; exists {
			return fmt.Errorf("duplicate runtime bundle target %q", target)
		}
		entries[target] = bundleEntry{target: target, source: sourcePath}
		return nil
	}
	addData := func(target string, data []byte) {
		target = filepath.ToSlash(target)
		entries[target] = bundleEntry{target: target, data: data}
	}
	addTree := func(sourceRoot string, targetRoot string) error {
		info, err := os.Stat(sourceRoot)
		if err != nil {
			if require {
				return err
			}
			return nil
		}
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", sourceRoot)
		}
		return filepath.WalkDir(sourceRoot, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(sourceRoot, path)
			if err != nil {
				return err
			}
			return addFile(path, filepath.Join(targetRoot, rel))
		})
	}

	if err := addTree(filepath.Join(source, "ffmpeg"), filepath.Join("runtime", "ffmpeg")); err != nil {
		return nil, err
	}
	if err := addTree(filepath.Join(source, "licenses"), filepath.Join("licenses", "media-runtime")); err != nil {
		return nil, err
	}
	if require {
		for _, item := range []struct {
			source string
			target string
		}{
			{filepath.Join(source, "licenses", "media-runtime-source-record.json"), filepath.Join("licenses", "media-runtime", "media-runtime-source-record.json")},
			{filepath.Join(source, "licenses", "media-runtime-build.txt"), filepath.Join("licenses", "media-runtime", "media-runtime-build.txt")},
			{filepath.Join(source, "licenses", "ffmpeg-buildconf.txt"), filepath.Join("licenses", "media-runtime", "ffmpeg-buildconf.txt")},
			{filepath.Join(source, "licenses", "mpv-meson-configure.txt"), filepath.Join("licenses", "media-runtime", "mpv-meson-configure.txt")},
			{filepath.Join(source, "licenses", "ffmpeg-local-changes.diff"), filepath.Join("licenses", "media-runtime", "ffmpeg-local-changes.diff")},
			{filepath.Join(source, "licenses", "mpv-local-changes.diff"), filepath.Join("licenses", "media-runtime", "mpv-local-changes.diff")},
		} {
			if err := addFile(item.source, item.target); err != nil {
				return nil, err
			}
		}
	}
	for _, pattern := range []string{
		filepath.Join(source, "*.dll"),
		filepath.Join(source, "mpv", "*.dll"),
		filepath.Join(source, "mpv", "bin", "*.dll"),
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		for _, match := range matches {
			if err := addFile(match, filepath.Base(match)); err != nil {
				return nil, err
			}
		}
	}
	for _, item := range []struct {
		source string
		target string
	}{
		{"LICENSE", filepath.Join("licenses", "LICENSE")},
		{"THIRD_PARTY_NOTICES.md", filepath.Join("licenses", "THIRD_PARTY_NOTICES.md")},
		{filepath.Join("docs", "dependency-sources.md"), filepath.Join("licenses", "dependency-sources.md")},
		{filepath.Join("build", "deps", "manifest.json"), filepath.Join("licenses", "dependency-manifest.json")},
	} {
		if err := addFile(item.source, item.target); err != nil {
			return nil, err
		}
	}
	addData(filepath.Join("runtime", "version.txt"), []byte(version+"\n"))

	out := make([]bundleEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].target < out[j].target
	})
	return out, nil
}

func writeRuntimeZip(zipPath string, entries []bundleEntry) ([]runtimeManifestFile, error) {
	out, err := os.Create(zipPath)
	if err != nil {
		return nil, err
	}
	zw := zip.NewWriter(out)
	files := make([]runtimeManifestFile, 0, len(entries))
	for _, entry := range entries {
		body, err := entry.bytes()
		if err != nil {
			_ = zw.Close()
			_ = out.Close()
			return nil, err
		}
		header := &zip.FileHeader{
			Name:     filepath.ToSlash(entry.target),
			Method:   zip.Deflate,
			Modified: time.Unix(0, 0).UTC(),
		}
		header.SetMode(0o644)
		writer, err := zw.CreateHeader(header)
		if err != nil {
			_ = zw.Close()
			_ = out.Close()
			return nil, err
		}
		if _, err := io.Copy(writer, bytes.NewReader(body)); err != nil {
			_ = zw.Close()
			_ = out.Close()
			return nil, err
		}
		sum := sha256.Sum256(body)
		files = append(files, runtimeManifestFile{
			Path:   filepath.ToSlash(entry.target),
			SHA256: hex.EncodeToString(sum[:]),
			Size:   int64(len(body)),
		})
	}
	if err := zw.Close(); err != nil {
		_ = out.Close()
		return nil, err
	}
	if err := out.Close(); err != nil {
		return nil, err
	}
	return files, nil
}

func (e bundleEntry) bytes() ([]byte, error) {
	if e.data != nil {
		return e.data, nil
	}
	return os.ReadFile(e.source)
}

func loadPrivateKeyPEM(keyEnv string, keyFile string) ([]byte, error) {
	if keyFile != "" {
		return os.ReadFile(keyFile)
	}
	value := strings.TrimSpace(os.Getenv(keyEnv))
	if value == "" {
		return nil, fmt.Errorf("%s is empty", keyEnv)
	}
	value = strings.ReplaceAll(value, `\n`, "\n")
	return []byte(value), nil
}

func parsePrivateKey(body []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(body)
	if block != nil {
		body = block.Bytes
	}
	keyAny, err := x509.ParsePKCS8PrivateKey(body)
	if err == nil {
		key, ok := keyAny.(ed25519.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("private key has wrong type %T", keyAny)
		}
		return key, nil
	}
	if decoded, decodeErr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(body))); decodeErr == nil {
		if len(decoded) == ed25519.PrivateKeySize {
			return ed25519.PrivateKey(decoded), nil
		}
	}
	if len(body) == ed25519.PrivateKeySize {
		return ed25519.PrivateKey(body), nil
	}
	return nil, err
}

func signFile(privateKey ed25519.PrivateKey, path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	sum := hash.Sum(nil)
	signature := ed25519.Sign(privateKey, sum)
	sigPath := path + ".sig"
	if err := os.WriteFile(sigPath, []byte(base64.StdEncoding.EncodeToString(signature)+"\n"), 0o644); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum), nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
