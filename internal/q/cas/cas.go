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

// A stringHasher adapts an already-computed hash string to the Hasher interface.
type stringHasher string

// Hash returns the stored hash string unchanged.
func (h stringHasher) Hash() string { return string(h) }

// NewFileSetHasher returns a Hasher for paths and their current contents. Path order does not matter.
//
// NewFileSetHasher reads each path as supplied; relative paths are resolved against the current process working directory. The hash identity includes each file's
// contents and the cleaned, slash-separated path string, so absolute paths make the hash depend on the absolute location. Prefer relative paths when the hash should
// be stable across machines.
//
// Use NewDirRelativeFileSetHasher when path identity should be relative to a base directory.
//
// Returns an error if, for instance, a path cannot be read.
func NewFileSetHasher(paths []string) (Hasher, error) {
	return newFileSetHasher(paths, nil)
}

// NewDirRelativeFileSetHasher returns a Hasher for paths whose path identity is based on each path's location relative to dir. It returns an error if any p in paths
// is outside dir.
//
// The paths are read as supplied, just as in NewFileSetHasher; dir is used only to compute the path identity for the hash and to validate that paths are within
// dir. Relative paths are resolved against the current process working directory.
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

// The newFileSetHasher function returns a Hasher for the current contents of the files named by paths.
//
// Paths are treated as a set: input order does not affect the hash, and duplicate path strings are ignored. If transform is non-nil, it derives the path identity
// used in the hash; files are still read from the original paths. The path identity is normalized to a cleaned, slash-separated form.
//
// It returns an error if transform fails or a file cannot be read.
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
	AbsRoot string // AbsRoot is the root directory under which records are stored. It must be non-empty before Store, Retrieve, or Delete is called.
}

// AdditionalInfo is saved and retrieved besides the primary Store payload.
type AdditionalInfo struct {
	// Seconds since Unix epoch.
	UnixTimestamp int `json:"unix_timestamp"`

	// Caller-supplied opaque paths. Caller may often try to align these with paths passed to, e.g., NewFileSetHasher, but this package does not verify them.
	Paths []string `json:"paths"`

	GitClean              bool   `json:"git_clean"`                         // True if computed with a clean git worktree.
	GitCommit             string `json:"git_commit"`                        // Git commit the metadata was computed against.
	GitMergeBase          string `json:"git_merge_base"`                    // Merge-base for GitCommit (if relevant).
	Recertified           bool   `json:"recertified,omitempty"`             // True when copied forward from a source record.
	RecertifiedFromHash   string `json:"recertified_from_hash,omitempty"`   // Source content hash.
	RecertifiedFromRecord string `json:"recertified_from_record,omitempty"` // Source CAS record.
}

// Options let callers supply AdditionalInfo fields if desired. Also exists for future Store extensibility.
type Options struct {
	AdditionalInfo // AdditionalInfo is optional provenance data stored alongside metadata when non-zero.
}

// A recordV1 is the JSON representation of a version 1 CAS record.
type recordV1 struct {
	Kind           string          `json:"kind"`                      // Kind identifies the record schema version.
	Metadata       json.RawMessage `json:"metadata"`                  // Metadata contains the caller-supplied JSON payload.
	AdditionalInfo *AdditionalInfo `json:"additional_info,omitempty"` // AdditionalInfo contains optional provenance data stored with the metadata.
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
	hash := hasher.Hash()
	if err := validateRecordKey(namespace, hash); err != nil {
		return err
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
	hash := hasher.Hash()
	if err := validateRecordKey(namespace, hash); err != nil {
		return false, AdditionalInfo{}, err
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

// Delete removes metadata for (namespace, hasher.Hash()).
//
// namespace must be filesystem-safe with no path separators. If the record does not exist, Delete returns nil.
func (db *DB) Delete(hasher Hasher, namespace string) error {
	if db.AbsRoot == "" {
		return errors.New("DB.AbsRoot is empty")
	}
	if hasher == nil {
		return errors.New("hasher is nil")
	}
	hash := hasher.Hash()
	if err := validateRecordKey(namespace, hash); err != nil {
		return err
	}

	if err := os.Remove(db.recordPath(namespace, hash)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// The recordPath method returns the filesystem path for namespace and hash under db.AbsRoot.
//
// The returned path is sharded as <AbsRoot>/<namespace>/<hash[0:2]>/<hash[2:]>. The caller must pass a validated record key with a hash at least three bytes long.
func (db *DB) recordPath(namespace, hash string) string {
	return filepath.Join(db.AbsRoot, namespace, hash[:2], hash[2:])
}

func validateRecordKey(namespace, hash string) error {
	if err := validatePathSegment("namespace", namespace); err != nil {
		return err
	}
	if err := validatePathSegment("hash", hash); err != nil {
		return err
	}
	if len(hash) < 3 {
		return fmt.Errorf("hash %q is too short", hash)
	}
	if err := validatePathSegment("hash directory segment", hash[:2]); err != nil {
		return err
	}
	if err := validatePathSegment("hash record segment", hash[2:]); err != nil {
		return err
	}
	return nil
}

func validatePathSegment(name, s string) error {
	if s == "" {
		return fmt.Errorf("%s is empty", name)
	}
	if s == "." || s == ".." {
		return fmt.Errorf("%s %q must not be a filesystem-special dot segment", name, s)
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
		ai.GitMergeBase == "" &&
		!ai.Recertified &&
		ai.RecertifiedFromHash == "" &&
		ai.RecertifiedFromRecord == ""
}
