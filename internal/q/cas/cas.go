package cas

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Hasher identifies a CAS record by hash.
type Hasher interface {
	// Hash must be filesystem-safe with no path separators.
	Hash() string
}

type stringHasher string

func (h stringHasher) Hash() string { return string(h) }

// NewFileSetHasher returns a Hasher for the given paths. Path order does not matter.
//
// Paths should be relative paths, as they are used to compute the hash (and should ideally be stable across computers).
//
// Returns an error if, for instance, a path cannot be read.
func NewFileSetHasher(paths []string) (Hasher, error) {
	return newFileSetHasher(paths, nil)
}

// NewDirRelativeFileSetHasher is like NewFileSetHasher, but the hash is based on the dir-relative portion of each p in paths. Returns an error if any p in paths
// is not in dir.
//
// This allows the group of files to be moved as a unit without affecting their hash.
func NewDirRelativeFileSetHasher(dir string, paths []string) (Hasher, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	transform := func(p string) (string, error) {
		absP, err := filepath.Abs(p)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(absDir, absP)
		if err != nil {
			return "", err
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("path %q is not in dir %q", p, dir)
		}
		return rel, nil
	}

	return newFileSetHasher(paths, transform)
}

// NewBytesHasher returns a Hasher for the bytes.
func NewBytesHasher(b []byte) Hasher {
	sum := sha256.Sum256(b)
	return stringHasher(hex.EncodeToString(sum[:]))
}

func newFileSetHasher(paths []string, transform func(string) (string, error)) (Hasher, error) {
	// Treat this as a set: order doesn't matter, and duplicates should not change the hash.
	pathsCopy := append([]string(nil), paths...)
	sort.Strings(pathsCopy)

	h := sha256.New()
	buf := make([]byte, 8)

	var last string
	for _, p := range pathsCopy {
		if p == last {
			continue
		}
		last = p

		hashPath := p
		if transform != nil {
			var err error
			hashPath, err = transform(p)
			if err != nil {
				return nil, err
			}
		}
		// Hash normalized paths so that equivalent relative paths are stable across platforms.
		hashPath = path.Clean(filepath.ToSlash(hashPath))

		binary.LittleEndian.PutUint64(buf, uint64(len(hashPath)))
		_, _ = h.Write(buf)
		_, _ = h.Write([]byte(hashPath))

		contents, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		binary.LittleEndian.PutUint64(buf, uint64(len(contents)))
		_, _ = h.Write(buf)
		_, _ = h.Write(contents)
	}

	return stringHasher(hex.EncodeToString(h.Sum(nil))), nil
}

// DB is a filesystem-backed metadata store rooted at AbsRoot.
type DB struct {
	AbsRoot string
}

// AdditionalInfo is saved and retrieved besides the primary Store payload.
type AdditionalInfo struct {
	// Seconds since Unix epoch.
	UnixTimestamp int

	// Caller-supplied opaque paths. Caller may often try to align these with paths passed to, e.g., NewFileSetHasher, but this package does not verify them.
	Paths []string

	GitClean     bool   // True if computed with a clean git worktree.
	GitCommit    string // Git commit the metadata was computed against.
	GitMergeBase string // Merge-base for GitCommit (if relevant).
}

// Options let callers supply AdditionalInfo fields if desired. Also exists for future Store extensibility.
type Options struct {
	AdditionalInfo
}

type recordV1 struct {
	Kind           string          `json:"kind"`
	Metadata       json.RawMessage `json:"metadata"`
	AdditionalInfo *AdditionalInfo `json:"additional_info,omitempty"`
}

// Store serializes jsonable as JSON (using json.Marshal) and stores it for (namespace, hasher.Hash()).
//
// namespace must be filesystem-safe with no path separators. If opts is passed with non-zero AdditionalInfo, the additional info is stored (in the same record as
// jsonable).
func (db *DB) Store(hasher Hasher, namespace string, jsonable any, opts *Options) error {
	if db.AbsRoot == "" {
		return errors.New("DB.AbsRoot is empty")
	}
	if hasher == nil {
		return errors.New("hasher is nil")
	}
	if err := validatePathSegment("namespace", namespace); err != nil {
		return err
	}
	hash := hasher.Hash()
	if err := validatePathSegment("hash", hash); err != nil {
		return err
	}
	if len(hash) < 3 {
		return fmt.Errorf("hash %q is too short", hash)
	}

	payload, err := json.Marshal(jsonable)
	if err != nil {
		return err
	}

	rec := recordV1{
		Kind:     "cas-record-v1",
		Metadata: json.RawMessage(payload),
	}
	if opts != nil && !isZeroAdditionalInfo(opts.AdditionalInfo) {
		ai := opts.AdditionalInfo
		rec.AdditionalInfo = &ai
	}

	out, err := json.Marshal(rec)
	if err != nil {
		return err
	}

	finalPath := db.recordPath(namespace, hash)
	if existing, err := os.ReadFile(finalPath); err == nil && bytes.Equal(existing, out) {
		return nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(finalPath), "cas-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()

	if err := tmp.Chmod(0o644); err != nil {
		return err
	}
	if _, err := tmp.Write(out); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpName, finalPath); err != nil {
		return err
	}
	return nil
}

// Retrieve loads metadata for (namespace, hasher.Hash()) into target. It returns whether metadata was found, additional info, and any error. Metadata not being
// found is not, by itself, an error.
//   - target must be a pointer that is passed to json.Unmarshal.
//   - namespace must be filesystem-safe with no path separators.
func (db *DB) Retrieve(hasher Hasher, namespace string, target any) (bool, AdditionalInfo, error) {
	if db.AbsRoot == "" {
		return false, AdditionalInfo{}, errors.New("DB.AbsRoot is empty")
	}
	if hasher == nil {
		return false, AdditionalInfo{}, errors.New("hasher is nil")
	}
	if err := validatePathSegment("namespace", namespace); err != nil {
		return false, AdditionalInfo{}, err
	}
	hash := hasher.Hash()
	if err := validatePathSegment("hash", hash); err != nil {
		return false, AdditionalInfo{}, err
	}
	if len(hash) < 3 {
		return false, AdditionalInfo{}, fmt.Errorf("hash %q is too short", hash)
	}

	b, err := os.ReadFile(db.recordPath(namespace, hash))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, AdditionalInfo{}, nil
		}
		return false, AdditionalInfo{}, err
	}

	var rec recordV1
	if err := json.Unmarshal(b, &rec); err != nil {
		return false, AdditionalInfo{}, err
	}
	if rec.Kind != "cas-record-v1" {
		return false, AdditionalInfo{}, fmt.Errorf("unknown record kind %q", rec.Kind)
	}
	if err := json.Unmarshal(rec.Metadata, target); err != nil {
		return false, AdditionalInfo{}, err
	}
	if rec.AdditionalInfo != nil {
		return true, *rec.AdditionalInfo, nil
	}
	return true, AdditionalInfo{}, nil
}

func (db *DB) recordPath(namespace, hash string) string {
	return filepath.Join(db.AbsRoot, namespace, hash[:2], hash[2:])
}

func validatePathSegment(name, s string) error {
	if s == "" {
		return fmt.Errorf("%s is empty", name)
	}
	// Disallow both separators to be safe cross-platform.
	if strings.Contains(s, "/") || strings.Contains(s, `\`) {
		return fmt.Errorf("%s %q must not contain path separators", name, s)
	}
	return nil
}

func isZeroAdditionalInfo(ai AdditionalInfo) bool {
	return ai.UnixTimestamp == 0 &&
		len(ai.Paths) == 0 &&
		!ai.GitClean &&
		ai.GitCommit == "" &&
		ai.GitMergeBase == ""
}
