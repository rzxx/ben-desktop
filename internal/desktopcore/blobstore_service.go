package desktopcore

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"lukechampine.com/blake3"
)

type BlobStoreService struct {
	root string
}

func NewBlobStoreService(root string) *BlobStoreService {
	return &BlobStoreService{root: strings.TrimSpace(root)}
}

func (s *BlobStoreService) Root() string {
	if s == nil {
		return ""
	}
	return s.root
}

func (s *BlobStoreService) Path(blobID string) (string, error) {
	parts := strings.SplitN(strings.TrimSpace(blobID), ":", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) != "b3" {
		return "", fmt.Errorf("invalid blob id")
	}
	hashHex := strings.ToLower(strings.TrimSpace(parts[1]))
	if len(hashHex) != 64 {
		return "", fmt.Errorf("invalid blob id")
	}
	return filepath.Join(s.root, "b3", hashHex[:2], hashHex[2:4], hashHex), nil
}

func (s *BlobStoreService) ArtworkPath(blobID, fileExt string) (string, error) {
	fileExt = normalizeArtworkFileExt(fileExt, "")
	if fileExt == "" {
		return "", fmt.Errorf("artwork file extension is required")
	}
	basePath, err := s.Path(blobID)
	if err != nil {
		return "", err
	}
	return basePath + fileExt, nil
}

func (s *BlobStoreService) StoreBytes(data []byte) (string, error) {
	blobID := s.IDForBytes(data)
	path, err := s.Path(blobID)
	if err != nil {
		return "", err
	}
	return blobID, writeBytes(path, data)
}

func (s *BlobStoreService) StoreArtworkBytes(data []byte, fileExt string) (string, error) {
	blobID := s.IDForBytes(data)
	path, err := s.ArtworkPath(blobID, fileExt)
	if err != nil {
		return "", err
	}
	return blobID, writeBytes(path, data)
}

func (s *BlobStoreService) ArtworkFilePath(blobID, fileExt string) (string, bool, error) {
	typedPath, err := s.ArtworkPath(blobID, fileExt)
	if err != nil {
		return "", false, err
	}
	if _, err := os.Stat(typedPath); err == nil {
		return typedPath, true, nil
	} else if !os.IsNotExist(err) {
		return "", false, err
	}
	return typedPath, false, nil
}

func (s *BlobStoreService) ReadVerified(blobID string) ([]byte, error) {
	path, err := s.Path(blobID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := s.VerifyID(blobID, data); err != nil {
		return nil, err
	}
	return data, nil
}

func (s *BlobStoreService) IDForBytes(data []byte) string {
	sum := blake3.Sum256(data)
	return "b3:" + hex.EncodeToString(sum[:])
}

func (s *BlobStoreService) VerifyID(blobID string, data []byte) error {
	if strings.TrimSpace(blobID) == "" {
		return fmt.Errorf("blob id is required")
	}
	if actual := s.IDForBytes(data); strings.TrimSpace(actual) != strings.TrimSpace(blobID) {
		return fmt.Errorf("blob hash mismatch")
	}
	return nil
}

func blobIDForBytes(data []byte) string {
	return NewBlobStoreService("").IDForBytes(data)
}

func verifyBlobIDBytes(blobID string, data []byte) error {
	return NewBlobStoreService("").VerifyID(blobID, data)
}

func writeBytes(path string, data []byte) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		if _, statErr := os.Stat(path); statErr == nil {
			_ = os.Remove(tempPath)
			return nil
		}
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}
