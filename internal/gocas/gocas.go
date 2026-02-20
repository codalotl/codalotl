package gocas

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/codalotl/codalotl/internal/codeunit"
	"github.com/codalotl/codalotl/internal/q/cas"
)

// Namespace is a logical partition + version for values stored in content-addressable storage (CAS).
//
// Namespace must be filesystem-safe (no path separators), because it is used as a directory name under the CAS root.
//
// Treat a Namespace like a schema/version string:
//   - Keep it stable for a given JSON shape + meaning.
//   - Bump it when the stored JSON schema or semantics change, to avoid decoding old data into a new type.
type Namespace string

const (
	// NamespaceSpecConforms stores results produced by spec conformance checks.
	//
	// Version suffix is bumped when the stored JSON schema or semantics change.
	NamespaceSpecConforms Namespace = "specconforms-1"
)

// DB stores and retrieves Go-package-adjacent metadata in content-addressable storage (CAS).
//
// Keys are derived from the contents of a code unit (see StoreOnCodeUnit) plus a Namespace. Values are stored as JSON.
//
// DB wraps cas.DB to add:
//   - keying based on codeunit.CodeUnit (file-content hashing)
//   - best-effort git metadata capture (returned as cas.AdditionalInfo)
//
// Most callers should use the methods on *DB, rather than calling methods on the embedded cas.DB directly.
type DB struct {
	// BaseDir is the project root used when hashing paths from a code unit.
	//
	// BaseDir must be absolute. All paths returned by unit.IncludedFiles() must be within BaseDir.
	//
	// Hashing is based on the BaseDir-relative portion of each path, so hashing the same project from different working directories produces the same keys.
	BaseDir string

	// DB is the underlying filesystem-backed metadata store.
	//
	// AbsRoot must be an absolute path and is the root directory where records are stored:
	//
	//	<AbsRoot>/<namespace>/<hash[0:2]>/<hash[2:]>
	cas.DB
}

// StoreOnCodeUnit stores jsonable for (unit, namespace).
//
// Storage key is content-addressed from unit.IncludedFiles() and their file contents (paths are interpreted relative to BaseDir), plus namespace.
//
// If any included file cannot be read, StoreOnCodeUnit returns an error.
//
// jsonable must be encodable by encoding/json (and is stored as JSON bytes).
//
// StoreOnCodeUnit returns an error only for "real" failures (I/O, JSON encoding, CAS write failures, etc). Lack of git information is not an error.
func (db *DB) StoreOnCodeUnit(unit *codeunit.CodeUnit, namespace Namespace, jsonable any) error {
	if db == nil {
		return errors.New("gocas DB is nil")
	}
	if unit == nil {
		return errors.New("code unit is nil")
	}
	if err := validateNamespace(namespace); err != nil {
		return err
	}
	if err := validateAbsDir("BaseDir", db.BaseDir); err != nil {
		return err
	}
	if err := validateAbsDir("cas.DB.AbsRoot", db.DB.AbsRoot); err != nil {
		return err
	}

	hasher, relPaths, err := db.hasherForCodeUnit(unit)
	if err != nil {
		return err
	}

	additionalInfo := cas.AdditionalInfo{
		UnixTimestamp: int(time.Now().Unix()),
		Paths:         relPaths,
	}
	db.bestEffortPopulateGitInfo(&additionalInfo)

	return db.DB.Store(hasher, string(namespace), jsonable, &cas.Options{
		AdditionalInfo: additionalInfo,
	})
}

// Retrieve loads the stored value for (unit, namespace) into target.
//
// ok reports whether a value existed. When ok is false, target is left unchanged.
//
// additionalInfo is returned from the underlying CAS layer and may include best-effort git metadata captured at store time. Most callers should treat AdditionalInfo
// as optional; see cas.AdditionalInfo field docs for details.
//
// Retrieve returns an error only for "real" failures (I/O, JSON decode, CAS read failures, etc).
func (db *DB) Retrieve(unit *codeunit.CodeUnit, namespace Namespace, target any) (ok bool, additionalInfo cas.AdditionalInfo, err error) {
	if db == nil {
		return false, cas.AdditionalInfo{}, errors.New("gocas DB is nil")
	}
	if unit == nil {
		return false, cas.AdditionalInfo{}, errors.New("code unit is nil")
	}
	if err := validateNamespace(namespace); err != nil {
		return false, cas.AdditionalInfo{}, err
	}
	if err := validateAbsDir("BaseDir", db.BaseDir); err != nil {
		return false, cas.AdditionalInfo{}, err
	}
	if err := validateAbsDir("cas.DB.AbsRoot", db.DB.AbsRoot); err != nil {
		return false, cas.AdditionalInfo{}, err
	}
	if target == nil {
		return false, cas.AdditionalInfo{}, errors.New("target is nil")
	}

	hasher, _, err := db.hasherForCodeUnit(unit)
	if err != nil {
		return false, cas.AdditionalInfo{}, err
	}

	var raw json.RawMessage
	ok, additionalInfo, err = db.DB.Retrieve(hasher, string(namespace), &raw)
	if err != nil || !ok {
		return ok, additionalInfo, err
	}

	if err := json.Unmarshal(raw, target); err != nil {
		return true, additionalInfo, err
	}
	return true, additionalInfo, nil
}

func validateNamespace(namespace Namespace) error {
	if namespace == "" {
		return errors.New("namespace is empty")
	}
	// Disallow both separators so this validation is stable across GOOS.
	if strings.ContainsAny(string(namespace), `/\`) {
		return fmt.Errorf("namespace %q contains a path separator", namespace)
	}
	return nil
}

func validateAbsDir(fieldName, p string) error {
	if p == "" {
		return fmt.Errorf("%s is empty", fieldName)
	}
	if !filepath.IsAbs(p) {
		return fmt.Errorf("%s must be an absolute path: %q", fieldName, p)
	}
	fi, err := os.Stat(p)
	if err != nil {
		return fmt.Errorf("stat %s: %w", fieldName, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory: %q", fieldName, p)
	}
	return nil
}

func (db *DB) hasherForCodeUnit(unit *codeunit.CodeUnit) (cas.Hasher, []string, error) {
	absPaths := unit.IncludedFiles()
	fileAbsPaths := make([]string, 0, len(absPaths))
	fileRelPaths := make([]string, 0, len(absPaths))
	for _, p := range absPaths {
		fi, err := os.Stat(p)
		if err != nil {
			return nil, nil, err
		}
		if fi.IsDir() {
			continue
		}

		rel, err := filepath.Rel(db.BaseDir, p)
		if err != nil {
			return nil, nil, err
		}
		if rel == ".." ||
			strings.HasPrefix(rel, ".."+string(filepath.Separator)) ||
			strings.HasPrefix(rel, "../") ||
			strings.HasPrefix(rel, `..\`) {
			return nil, nil, fmt.Errorf("included file %q is outside BaseDir %q", p, db.BaseDir)
		}

		fileAbsPaths = append(fileAbsPaths, p)
		fileRelPaths = append(fileRelPaths, rel)
	}

	hasher, err := cas.NewDirRelativeFileSetHasher(db.BaseDir, fileAbsPaths)
	if err != nil {
		return nil, nil, err
	}
	return hasher, fileRelPaths, nil
}

func (db *DB) bestEffortPopulateGitInfo(ai *cas.AdditionalInfo) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return
	}

	commit, err := gitOutput(db.BaseDir, gitPath, "rev-parse", "HEAD")
	if err != nil {
		return
	}
	ai.GitCommit = commit

	status, err := gitOutput(db.BaseDir, gitPath, "status", "--porcelain")
	if err != nil {
		return
	}
	ai.GitClean = (status == "")

	mergeBase, err := gitOutput(db.BaseDir, gitPath, "merge-base", "HEAD", "@{upstream}")
	if err != nil {
		return
	}
	ai.GitMergeBase = mergeBase
}

func gitOutput(dir, gitPath string, args ...string) (string, error) {
	cmd := exec.Command(gitPath, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return trimTrailingNewline(string(out)), nil
}

func trimTrailingNewline(s string) string {
	s = strings.TrimSuffix(s, "\n")
	s = strings.TrimSuffix(s, "\r")
	return s
}
