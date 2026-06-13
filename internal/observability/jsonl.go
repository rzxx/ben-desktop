package observability

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const jsonlNewline = "\n"

type jsonlWriter struct {
	mu       sync.Mutex
	path     string
	file     *os.File
	maxBytes int64
	written  int64
}

func newJSONLWriter(path string, maxBytes int64) (*jsonlWriter, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("jsonl path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return &jsonlWriter{path: path, file: file, maxBytes: maxBytes, written: info.Size()}, nil
}

func (w *jsonlWriter) WriteJSON(value any) (int, error) {
	if w == nil {
		return 0, nil
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return 0, err
	}
	payload = append(payload, jsonlNewline...)
	if w.maxBytes > 0 && int64(len(payload)) > w.maxBytes {
		return 0, fmt.Errorf("jsonl record exceeds file limit")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.maxBytes > 0 && w.written+int64(len(payload)) > w.maxBytes {
		return 0, fmt.Errorf("jsonl writer limit exceeded")
	}
	n, err := w.file.Write(payload)
	w.written += int64(n)
	return n, err
}

func (w *jsonlWriter) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *jsonlWriter) BytesWritten() int64 {
	if w == nil {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.written
}

type rotatingJSONLWriter struct {
	mu       sync.Mutex
	dir      string
	prefix   string
	maxBytes int64
	maxFiles int
	current  *jsonlWriter
}

func newRotatingJSONLWriter(dir, prefix string, maxBytes int64, maxFiles int) (*rotatingJSONLWriter, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, fmt.Errorf("log directory is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	w := &rotatingJSONLWriter{
		dir:      dir,
		prefix:   strings.TrimSpace(prefix),
		maxBytes: maxBytes,
		maxFiles: maxFiles,
	}
	if w.prefix == "" {
		w.prefix = "app"
	}
	if w.maxBytes <= 0 {
		w.maxBytes = 10 * 1024 * 1024
	}
	if w.maxFiles <= 0 {
		w.maxFiles = 10
	}
	if err := w.rotateLocked(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *rotatingJSONLWriter) WriteJSON(value any) (int, error) {
	if w == nil {
		return 0, nil
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return 0, err
	}
	payload = append(payload, jsonlNewline...)

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.current == nil || w.current.BytesWritten()+int64(len(payload)) > w.maxBytes {
		if err := w.rotateLocked(); err != nil {
			return 0, err
		}
	}
	w.current.mu.Lock()
	n, err := w.current.file.Write(payload)
	w.current.written += int64(n)
	w.current.mu.Unlock()
	return n, err
}

func (w *rotatingJSONLWriter) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.current == nil {
		return nil
	}
	err := w.current.Close()
	w.current = nil
	return err
}

func (w *rotatingJSONLWriter) rotateLocked() error {
	if w.current != nil {
		if err := w.current.Close(); err != nil {
			return err
		}
	}
	name := fmt.Sprintf("%s-%s.jsonl", w.prefix, time.Now().UTC().Format("20060102-150405.000000000"))
	writer, err := newJSONLWriter(filepath.Join(w.dir, name), w.maxBytes)
	if err != nil {
		return err
	}
	w.current = writer
	return w.pruneLocked()
}

func (w *rotatingJSONLWriter) pruneLocked() error {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return err
	}
	type fileInfo struct {
		path    string
		modTime time.Time
	}
	files := make([]fileInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, w.prefix+"-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, fileInfo{path: filepath.Join(w.dir, name), modTime: info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.Before(files[j].modTime)
	})
	for len(files) > w.maxFiles {
		if err := os.Remove(files[0].path); err != nil && !os.IsNotExist(err) {
			return err
		}
		files = files[1:]
	}
	return nil
}
