package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	relaydNonrootUID = 65532
	relaydNonrootGID = 65532
)

func prepareProcessPrivileges(opts relaydOptions) error {
	if os.Geteuid() != 0 {
		return nil
	}
	if err := claimStoragePaths(opts); err != nil {
		return err
	}
	if err := syscall.Setgroups([]int{relaydNonrootGID}); err != nil {
		return fmt.Errorf("set supplemental groups: %w", err)
	}
	if err := syscall.Setgid(relaydNonrootGID); err != nil {
		return fmt.Errorf("drop group privileges: %w", err)
	}
	if err := syscall.Setuid(relaydNonrootUID); err != nil {
		return fmt.Errorf("drop user privileges: %w", err)
	}
	if os.Geteuid() == 0 {
		return fmt.Errorf("process is still root after privilege drop")
	}
	log.Printf("dropped privileges to uid=%d gid=%d", relaydNonrootUID, relaydNonrootGID)
	return nil
}

func claimStoragePaths(opts relaydOptions) error {
	for _, dir := range uniqueStorageParents(opts.DBPath, opts.IdentityKeyPath) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create storage directory %q: %w", dir, err)
		}
		if err := os.Chown(dir, relaydNonrootUID, relaydNonrootGID); err != nil {
			return fmt.Errorf("chown storage directory %q: %w", dir, err)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("chmod storage directory %q: %w", dir, err)
		}
	}
	for _, path := range sqliteStorageFiles(opts.DBPath) {
		if err := claimExistingStorageFile(path, 0o600); err != nil {
			return err
		}
	}
	if err := claimExistingStorageFile(opts.IdentityKeyPath, 0o600); err != nil {
		return err
	}
	return nil
}

func uniqueStorageParents(paths ...string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		dir := filepath.Dir(path)
		if dir == "" || dir == "." {
			continue
		}
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		out = append(out, dir)
	}
	return out
}

func sqliteStorageFiles(dbPath string) []string {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		return nil
	}
	return []string{dbPath, dbPath + "-wal", dbPath + "-shm"}
}

func claimExistingStorageFile(path string, mode os.FileMode) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat storage file %q: %w", path, err)
	}
	if err := os.Chown(path, relaydNonrootUID, relaydNonrootGID); err != nil {
		return fmt.Errorf("chown storage file %q: %w", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("chmod storage file %q: %w", path, err)
	}
	return nil
}
