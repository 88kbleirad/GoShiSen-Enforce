// Package store implements durable policy storage. Policies are stored as
// immutable, versioned YAML files (<version>.yaml); the active policy is a
// small atomically-updated JSON pointer (active.json). Writes are
// write-temp-fsync-rename-fsync, so a crash at any point leaves either the
// old active pointer or the new one — never a torn file.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ActivePointer is the on-disk "which policy is active" record.
type ActivePointer struct {
	// Version is the active policy version.
	Version string `json:"version"`
	// Filename is the policy file name (redundant with Version; kept
	// for operator readability).
	Filename string `json:"filename"`
}

// Store is a directory-backed policy store.
type Store struct {
	dir string
}

// Open validates/creates the store directory.
func Open(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("store: empty directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("store: create dir %s: %w", dir, err)
	}
	return &Store{dir: dir}, nil
}

// OpenReadOnly opens an existing store directory without creating it.
// It fails if the directory does not exist, so a read-only caller (e.g.
// `policy dry-run`) never materializes filesystem state.
func OpenReadOnly(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("store: empty directory")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("store: %s is not a directory", dir)
	}
	return &Store{dir: dir}, nil
}

// Dir returns the store directory.
func (s *Store) Dir() string { return s.dir }

// VersionPath returns the absolute path of a policy file.
func (s *Store) VersionPath(version string) string {
	return filepath.Join(s.dir, sanitizeVersion(version)+".yaml")
}

// sanitizeVersion restricts version strings to safe file-name characters.
func sanitizeVersion(v string) string {
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		c := v[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// ValidateVersion rejects unsafe version strings before they reach the
// filesystem.
func ValidateVersion(v string) error {
	if v == "" {
		return errors.New("store: empty version")
	}
	if sanitizeVersion(v) != v {
		return fmt.Errorf("store: version %q contains unsafe characters (allow a-z0-9-_.)", v)
	}
	if len(v) > 64 {
		return fmt.Errorf("store: version too long")
	}
	return nil
}

// WritePolicy stores a policy under version with atomic file semantics:
// temp file in the same directory, fsync, rename over the target,
// fsync the directory. Existing files are never modified (immutable).
func (s *Store) WritePolicy(version string, data []byte) error {
	if err := ValidateVersion(version); err != nil {
		return err
	}
	path := s.VersionPath(version)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("store: policy %q already exists", version)
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("store: create temp: %w", err)
	}
	cleanup := func() { _ = os.Remove(tmp) }
	if _, err := f.Write(data); err != nil {
		f.Close()
		cleanup()
		return fmt.Errorf("store: write temp: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		cleanup()
		return fmt.Errorf("store: fsync temp: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return fmt.Errorf("store: close temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		cleanup()
		return fmt.Errorf("store: rename: %w", err)
	}
	if err := syncDir(s.dir); err != nil {
		return fmt.Errorf("store: fsync dir: %w", err)
	}
	return nil
}

// SetActive atomically updates the active pointer. It does not require
// the policy file to exist — the caller ensures durability ordering.
func (s *Store) SetActive(version string) error {
	if err := ValidateVersion(version); err != nil {
		return err
	}
	ptr := ActivePointer{Version: version, Filename: filepath.Base(s.VersionPath(version))}
	data, err := json.MarshalIndent(ptr, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWrite(filepath.Join(s.dir, "active.json"), data)
}

// Active returns the active version ("", empty if none).
func (s *Store) Active() (string, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, "active.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("store: read active: %w", err)
	}
	var ptr ActivePointer
	if err := json.Unmarshal(data, &ptr); err != nil {
		return "", fmt.Errorf("store: parse active: %w", err)
	}
	return ptr.Version, nil
}

// ReadPolicy returns a stored policy's raw bytes.
func (s *Store) ReadPolicy(version string) ([]byte, error) {
	if err := ValidateVersion(version); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.VersionPath(version))
	if err != nil {
		return nil, fmt.Errorf("store: read policy %q: %w", version, err)
	}
	return data, nil
}

// List returns stored policy versions sorted lexically.
func (s *Store) List() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("store: list: %w", err)
	}
	var versions []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".yaml") {
			continue
		}
		v := strings.TrimSuffix(name, ".yaml")
		if s, err := strconv.Atoi(v); err == nil && s >= 0 {
			versions = append(versions, v)
		}
	}
	sort.Strings(versions)
	return versions, nil
}

// NextVersion returns the next numeric policy version to assign (max
// existing version + 1, or "1" when the store is empty).
func (s *Store) NextVersion() (string, error) {
	versions, err := s.List()
	if err != nil {
		return "", err
	}
	max := 0
	for _, v := range versions {
		n, err := strconv.Atoi(v)
		if err != nil {
			continue
		}
		if n > max {
			max = n
		}
	}
	return strconv.Itoa(max + 1), nil
}

func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("store: create temp: %w", err)
	}
	cleanup := func() { _ = os.Remove(tmp) }
	if _, err := f.Write(data); err != nil {
		f.Close()
		cleanup()
		return fmt.Errorf("store: write temp: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		cleanup()
		return fmt.Errorf("store: fsync temp: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return fmt.Errorf("store: close temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		cleanup()
		return fmt.Errorf("store: rename: %w", err)
	}
	return syncDir(filepath.Dir(path))
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
