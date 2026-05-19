package gocas

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/codalotl/codalotl/internal/codeunit"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/q/cas"
)

// EnvCASDB is the environment variable that overrides the default CAS root.
const EnvCASDB = "CODALOTL_CAS_DB"

const casRecordKind = "cas-record-v1"
const defaultPruneSupersededAgeDays = 30

var errInvalidCASRecord = errors.New("invalid CAS record")

// Namespace is a logical partition + version for values stored in content-addressable storage (CAS).
//
// Namespace must be filesystem-safe (no path separators), because it is used as a directory name under the CAS root.
//
// Treat a Namespace like a schema/version string:
//   - Keep it stable for a given JSON shape + meaning.
//   - Bump it when the stored JSON schema or semantics change, to avoid decoding old data into a new type.
type Namespace string

// HashMode selects which files participate in a Go CAS hash.
type HashMode string

const (
	HashModePackage  HashMode = "package"   // HashModePackage hashes package Go files and package-local SPEC.md.
	HashModeCodeUnit HashMode = "code-unit" // HashModeCodeUnit hashes the package's default Go code unit.
)

// NamespaceSpec describes a CAS namespace.
type NamespaceSpec struct {
	Name     string
	Version  int
	HashMode HashMode
}

// Namespace returns the versioned filesystem namespace, such as "specconforms-1".
func (spec NamespaceSpec) Namespace() Namespace {
	return Namespace(fmt.Sprintf("%s-%d", spec.Name, spec.Version))
}

// DB stores and retrieves Go-package-adjacent and code-unit-adjacent metadata in content-addressable storage (CAS).
//
// Keys are derived from a gocode.Package and NamespaceSpec. Values are stored as JSON.
//
// DB wraps cas.DB to add:
//   - keying based on package or default-code-unit files (file-content hashing)
//   - best-effort git metadata capture (returned as cas.AdditionalInfo)
//
// Most callers should use the methods on *DB, rather than calling methods on the embedded cas.DB directly.
type DB struct {
	// BaseDir is the project root used when hashing package and code-unit file paths.
	//
	// BaseDir must be absolute. All hashed package and code-unit file paths must be within BaseDir.
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

// PackageRecordSummary describes one CAS record relevant to a Go package.
type PackageRecordSummary struct {
	// Hash is the CAS hash used as the record key within the namespace.
	Hash string

	// Time is the best-effort record time used for ordering records and pruning old superseded records.
	//
	// It prefers the git commit time for the commit that added the CAS record file. If that is unavailable, it falls back to AdditionalInfo.UnixTimestamp, then to the
	// CAS record file modification time.
	Time time.Time

	// AdditionalInfo is the CAS metadata stored beside the primary JSON payload.
	AdditionalInfo cas.AdditionalInfo
}

// PackageSummary describes current and prior CAS state for a package in one namespace.
type PackageSummary struct {
	// Current is non-nil when a CAS record exists for the package's current contents.
	Current *PackageRecordSummary

	// PriorInvalidated is the most relevant older matching record when Current is nil.
	PriorInvalidated *PackageRecordSummary

	// ChurnPercent is the best-effort changed-line percentage versus the newest prior record with a verified git baseline.
	ChurnPercent *float64
}

// PackageRecertificationStatus describes the outcome of package recertification.
type PackageRecertificationStatus string

const (
	PackageRecertificationStatusCurrent     PackageRecertificationStatus = "current"
	PackageRecertificationStatusRecertified PackageRecertificationStatus = "recertified"
	PackageRecertificationStatusNoPrior     PackageRecertificationStatus = "no-prior"
)

// PackageRecertificationResult describes a package recertification attempt.
type PackageRecertificationResult struct {
	Status       PackageRecertificationStatus
	CurrentHash  string
	SourceHash   string
	SourceRecord string
	Warnings     []string
}

// PruneOptions configures CAS record pruning.
type PruneOptions struct {
	// SupersededAgeDays removes superseded records older than this many days.
	//
	// If zero, Prune uses its default retention age, currently 30 days. Negative values are invalid.
	SupersededAgeDays int
}

// PruneResult summarizes deleted CAS records.
type PruneResult struct {
	DeletedPriorVersionRecords int
	DeletedSupersededRecords   int
}

// RootDirForBaseDir returns the absolute CAS root for baseDir.
func RootDirForBaseDir(baseDir string) (string, error) {
	if envRoot, ok := os.LookupEnv(EnvCASDB); ok {
		if envRoot == "" {
			return "", fmt.Errorf("%s is empty", EnvCASDB)
		}
		absRoot, err := filepath.Abs(envRoot)
		if err != nil {
			return "", fmt.Errorf("resolve %s: %w", EnvCASDB, err)
		}
		return absRoot, nil
	}

	if baseDir == "" {
		return "", errors.New("baseDir is empty")
	}
	absBaseDir, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("resolve baseDir: %w", err)
	}

	gitRoot, err := nearestGitRoot(absBaseDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(gitRoot, ".codalotl", "cas"), nil
}

// NewDBForBaseDir returns a Go-aware CAS database for baseDir.
//
// BaseDir and the underlying CAS root are absolute.
func NewDBForBaseDir(baseDir string) (*DB, error) {
	if baseDir == "" {
		return nil, errors.New("baseDir is empty")
	}
	absBaseDir, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, fmt.Errorf("resolve baseDir: %w", err)
	}

	absRoot, err := RootDirForBaseDir(absBaseDir)
	if err != nil {
		return nil, err
	}

	return &DB{
		BaseDir: absBaseDir,
		DB: cas.DB{
			AbsRoot: absRoot,
		},
	}, nil
}

// Store stores jsonable for (pkg, spec).
//
// Storage is keyed by the pair (spec.Namespace(), content hash). The content hash is computed from files selected by spec.HashMode; spec.Namespace() is passed to
// the underlying CAS as the namespace and is not mixed into the hash bytes. Paths are interpreted relative to BaseDir.
//
// HashModePackage uses package Go source files, package test files, and package-local SPEC.md.
//
// HashModeCodeUnit uses the default Go code unit rooted at pkg.
//
// Key derivation ignores duplicate absolute paths and directories, requires all remaining files to be within BaseDir, and sorts files by their BaseDir-relative
// paths before hashing.
//
// The selected files are hashed with BaseDir-relative path identity, so both file contents and their BaseDir-relative paths participate in the content hash.
//
// If any included file cannot be read, Store returns an error.
//
// jsonable must be encodable by encoding/json (and is stored as JSON bytes).
//
// Store does not return the derived hash or filesystem path of the stored record. Use Retrieve to confirm a value can be loaded later.
//
// Store returns an error only for "real" failures (I/O, JSON encoding, CAS write failures, etc). Lack of git information is not an error.
func (db *DB) Store(pkg *gocode.Package, spec NamespaceSpec, jsonable any) error {
	if pkg == nil {
		return errors.New("package is nil")
	}
	namespace, hasher, relPaths, err := db.hasherForValidatedPackageSpec(pkg, spec)
	if err != nil {
		return err
	}

	return db.store(namespace, hasher, relPaths, jsonable)
}

// Retrieve loads the stored value for (pkg, spec) into target.
//
// ok reports whether a value existed. When ok is false, target is left unchanged.
//
// additionalInfo is returned from the underlying CAS layer and may include best-effort git metadata captured at store time. Most callers should treat AdditionalInfo
// as optional; see cas.AdditionalInfo field docs for details.
//
// Retrieve returns an error only for "real" failures (I/O, JSON decode, CAS read failures, etc).
func (db *DB) Retrieve(pkg *gocode.Package, spec NamespaceSpec, target any) (ok bool, additionalInfo cas.AdditionalInfo, err error) {
	if pkg == nil {
		return false, cas.AdditionalInfo{}, errors.New("package is nil")
	}
	if target == nil {
		return false, cas.AdditionalInfo{}, errors.New("target is nil")
	}
	namespace, hasher, _, err := db.hasherForValidatedPackageSpec(pkg, spec)
	if err != nil {
		return false, cas.AdditionalInfo{}, err
	}

	return db.retrieve(namespace, hasher, target)
}

// SummarizePackage returns current and prior CAS record state for (pkg, spec).
//
// It uses the same hash mode and file selection as Store and Retrieve. Missing CAS roots or namespaces are treated as empty stores. Corrupt or unrelated prior records
// are skipped, while errors looking up the current hash are returned.
func (db *DB) SummarizePackage(pkg *gocode.Package, spec NamespaceSpec) (PackageSummary, error) {
	if pkg == nil {
		return PackageSummary{}, errors.New("package is nil")
	}
	namespace, hasher, relPaths, err := db.hasherForValidatedPackageSpec(pkg, spec)
	if err != nil {
		return PackageSummary{}, err
	}
	currentHash := hasher.Hash()

	namespaceDir := db.namespaceDir(namespace)
	if _, err := os.Stat(namespaceDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return PackageSummary{}, nil
		}
		return PackageSummary{}, fmt.Errorf("stat CAS namespace: %w", err)
	}

	var raw json.RawMessage
	ok, additionalInfo, err := db.retrieve(namespace, hasher, &raw)
	if err != nil {
		return PackageSummary{}, err
	}
	if ok {
		recordPath, _ := db.recordPath(namespace, currentHash)
		return PackageSummary{
			Current: db.packageRecordSummary(currentHash, additionalInfo, recordPath),
		}, nil
	}

	priorRecords := db.priorInvalidatedPackageRecords(namespace, currentHash, pkg, relPaths)
	var prior *PackageRecordSummary
	if len(priorRecords) > 0 {
		prior = priorRecords[0].summary
	}
	summary := PackageSummary{
		PriorInvalidated: prior,
	}
	summary.ChurnPercent = db.churnPercent(priorRecords, relPaths)
	return summary, nil
}

// RecertifyPackage asserts that pkg's current contents remain compliant with a recently invalidated CAS record for spec.
//
// If current package contents already have a CAS record, RecertifyPackage is a no-op. If there is no matching prior invalidated record, it returns a no-prior result.
// Otherwise it copies the most recent matching prior record payload to the current content hash, updates AdditionalInfo for current package state, marks the new
// record as recertified, and leaves existing records unchanged.
func (db *DB) RecertifyPackage(pkg *gocode.Package, spec NamespaceSpec) (PackageRecertificationResult, error) {
	if pkg == nil {
		return PackageRecertificationResult{}, errors.New("package is nil")
	}
	namespace, hasher, relPaths, err := db.hasherForValidatedPackageSpec(pkg, spec)
	if err != nil {
		return PackageRecertificationResult{}, err
	}

	result := PackageRecertificationResult{
		CurrentHash: hasher.Hash(),
	}

	namespaceDir := db.namespaceDir(namespace)
	if _, err := os.Stat(namespaceDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			result.Status = PackageRecertificationStatusNoPrior
			return result, nil
		}
		return result, fmt.Errorf("stat CAS namespace: %w", err)
	}

	var raw json.RawMessage
	ok, _, err := db.retrieve(namespace, hasher, &raw)
	if err != nil {
		return result, err
	}
	if ok {
		result.Status = PackageRecertificationStatusCurrent
		return result, nil
	}

	priorRecords := db.priorInvalidatedPackageRecords(namespace, result.CurrentHash, pkg, relPaths)
	if len(priorRecords) == 0 {
		result.Status = PackageRecertificationStatusNoPrior
		return result, nil
	}

	source := priorRecords[0]
	if source == nil || source.summary == nil {
		result.Status = PackageRecertificationStatusNoPrior
		return result, nil
	}
	sourceRecord, err := readFullCASRecordFile(source.recordPath)
	if err != nil {
		return result, fmt.Errorf("read source CAS record: %w", err)
	}

	sourceRecordID, ok := recordID(namespace, source.summary.Hash)
	if !ok {
		return result, fmt.Errorf("invalid source CAS hash %q", source.summary.Hash)
	}

	additionalInfo := cas.AdditionalInfo{
		UnixTimestamp:         int(time.Now().Unix()),
		Paths:                 relPaths,
		Recertified:           true,
		RecertifiedFromHash:   source.summary.Hash,
		RecertifiedFromRecord: sourceRecordID,
	}
	db.bestEffortPopulateGitInfo(&additionalInfo)

	if err := db.DB.Store(hasher, string(namespace), sourceRecord.Metadata, &cas.Options{AdditionalInfo: additionalInfo}); err != nil {
		return result, err
	}

	result.Status = PackageRecertificationStatusRecertified
	result.SourceHash = source.summary.Hash
	result.SourceRecord = sourceRecordID
	result.Warnings = db.recertificationWarnings(additionalInfo, source.summary, priorRecords, relPaths)
	return result, nil
}

// Prune removes obsolete CAS records for active namespace specs and known packages.
//
// A missing CAS root is treated as an empty store.
//
// Prune first removes prior namespace-version records selected from specs. A CAS namespace directory named "<Name>-<version>" is prior to a spec when Name matches
// spec.Name and version is positive and less than spec.Version. Prior-version pruning is namespace-wide: it deletes valid CAS record files in those directories
// without filtering by packages, age, or HashMode.
//
// Prune then removes superseded records for the exact active namespaces in specs and packages. A record is superseded only when it matches a supplied package, is
// older than the configured age by PackageRecordSummary.Time, has a newer matching package record, and is not protected as the current package hash or latest recertification
// provenance.
func (db *DB) Prune(specs []NamespaceSpec, packages []*gocode.Package, opts PruneOptions) (PruneResult, error) {
	if db == nil {
		return PruneResult{}, errors.New("gocas DB is nil")
	}
	if err := validateAbsDir("BaseDir", db.BaseDir); err != nil {
		return PruneResult{}, err
	}
	if err := validateAbsPath("cas.DB.AbsRoot", db.DB.AbsRoot); err != nil {
		return PruneResult{}, err
	}
	if opts.SupersededAgeDays < 0 {
		return PruneResult{}, fmt.Errorf("superseded age days must be non-negative: %d", opts.SupersededAgeDays)
	}
	supersededAgeDays := opts.SupersededAgeDays
	if supersededAgeDays == 0 {
		supersededAgeDays = defaultPruneSupersededAgeDays
	}

	activeSpecs := make(map[string]NamespaceSpec, len(specs))
	for _, spec := range specs {
		if err := validateNamespaceSpec(spec); err != nil {
			return PruneResult{}, err
		}
		if existing, ok := activeSpecs[spec.Name]; ok && existing.Version != spec.Version {
			return PruneResult{}, fmt.Errorf("multiple active versions for namespace %q: %d and %d", spec.Name, existing.Version, spec.Version)
		}
		activeSpecs[spec.Name] = spec
	}
	for _, pkg := range packages {
		if pkg == nil {
			return PruneResult{}, errors.New("package is nil")
		}
	}

	if fi, err := os.Stat(db.DB.AbsRoot); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return PruneResult{}, nil
		}
		return PruneResult{}, fmt.Errorf("stat CAS root: %w", err)
	} else if !fi.IsDir() {
		return PruneResult{}, fmt.Errorf("cas.DB.AbsRoot is not a directory: %q", db.DB.AbsRoot)
	}

	var result PruneResult
	deletedPriorVersions, err := db.prunePriorNamespaceVersions(activeSpecs)
	if err != nil {
		return result, err
	}
	result.DeletedPriorVersionRecords = deletedPriorVersions

	deletedSuperseded, err := db.pruneSupersededRecords(specs, packages, time.Duration(supersededAgeDays)*24*time.Hour)
	if err != nil {
		return result, err
	}
	result.DeletedSupersededRecords = deletedSuperseded
	return result, nil
}

func (db *DB) prunePriorNamespaceVersions(activeSpecs map[string]NamespaceSpec) (int, error) {
	entries, err := os.ReadDir(db.DB.AbsRoot)
	if err != nil {
		return 0, fmt.Errorf("read CAS root: %w", err)
	}

	deleted := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		for _, spec := range activeSpecs {
			if !isPriorNamespaceVersion(entry.Name(), spec) {
				continue
			}
			n, err := db.pruneNamespaceRecordFiles(Namespace(entry.Name()))
			if err != nil {
				return deleted, err
			}
			deleted += n
			break
		}
	}
	return deleted, nil
}

func isPriorNamespaceVersion(dirName string, spec NamespaceSpec) bool {
	suffix, ok := strings.CutPrefix(dirName, spec.Name+"-")
	if !ok || suffix == "" {
		return false
	}
	version, err := parseNonNegativeInt(suffix)
	if err != nil || version <= 0 {
		return false
	}
	return version < spec.Version
}

func (db *DB) pruneNamespaceRecordFiles(namespace Namespace) (int, error) {
	namespaceDir := db.namespaceDir(namespace)
	recordPaths := []string{}
	err := filepath.WalkDir(namespaceDir, func(recordPath string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if _, ok := recordHashFromPath(namespaceDir, recordPath); ok {
			recordPaths = append(recordPaths, recordPath)
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("walk prior CAS namespace: %w", err)
	}

	deleted := 0
	for _, recordPath := range recordPaths {
		removed, err := removeCASRecordPath(recordPath)
		if err != nil {
			return deleted, err
		}
		if removed {
			deleted++
			cleanupCASRecordDirs(namespaceDir, recordPath)
		}
	}
	_ = os.Remove(namespaceDir)
	return deleted, nil
}

func (db *DB) pruneSupersededRecords(specs []NamespaceSpec, packages []*gocode.Package, supersededAge time.Duration) (int, error) {
	protected, err := db.pruneProtectedHashes(specs, packages)
	if err != nil {
		return 0, err
	}

	cutoff := time.Now().Add(-supersededAge)
	deletedPaths := map[string]struct{}{}
	deleted := 0
	for _, spec := range specs {
		namespace := spec.Namespace()
		namespaceDir := db.namespaceDir(namespace)
		if _, err := os.Stat(namespaceDir); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return deleted, fmt.Errorf("stat CAS namespace: %w", err)
		}

		for _, pkg := range packages {
			_, _, relPaths, err := db.hasherForValidatedPackageSpec(pkg, spec)
			if err != nil {
				return deleted, err
			}

			records := db.packageRecordsForPrune(namespace, pkg, relPaths)
			for _, record := range records {
				if record == nil || record.summary == nil {
					continue
				}
				if _, ok := deletedPaths[record.recordPath]; ok {
					continue
				}
				if pruneHashProtected(protected, namespace, record.summary.Hash) {
					continue
				}
				if !recordOlderThan(record.summary, cutoff) || !hasNewerPackageRecord(records, record) {
					continue
				}

				removed, err := removeCASRecordPath(record.recordPath)
				if err != nil {
					return deleted, err
				}
				if removed {
					deletedPaths[record.recordPath] = struct{}{}
					deleted++
					cleanupCASRecordDirs(namespaceDir, record.recordPath)
				}
			}
		}
	}
	return deleted, nil
}

func (db *DB) pruneProtectedHashes(specs []NamespaceSpec, packages []*gocode.Package) (map[Namespace]map[string]struct{}, error) {
	protected := map[Namespace]map[string]struct{}{}
	addProtected := func(namespace Namespace, hash string) {
		if _, _, ok := splitRecordHash(hash); !ok {
			return
		}
		if _, ok := protected[namespace]; !ok {
			protected[namespace] = map[string]struct{}{}
		}
		protected[namespace][hash] = struct{}{}
	}

	for _, spec := range specs {
		namespace := spec.Namespace()
		for _, pkg := range packages {
			_, hasher, _, err := db.hasherForValidatedPackageSpec(pkg, spec)
			if err != nil {
				return nil, err
			}

			currentHash := hasher.Hash()
			addProtected(namespace, currentHash)

			currentRecordPath, ok := db.recordPath(namespace, currentHash)
			if !ok {
				continue
			}
			record, err := readFullCASRecordFile(currentRecordPath)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) || errors.Is(err, errInvalidCASRecord) {
					continue
				}
				return nil, fmt.Errorf("read current CAS record: %w", err)
			}
			if sourceHash, ok := recertificationSourceHash(namespace, record.AdditionalInfo); ok {
				addProtected(namespace, sourceHash)
			}
		}
	}
	return protected, nil
}

func pruneHashProtected(protected map[Namespace]map[string]struct{}, namespace Namespace, hash string) bool {
	if len(protected) == 0 {
		return false
	}
	hashes, ok := protected[namespace]
	if !ok {
		return false
	}
	_, ok = hashes[hash]
	return ok
}

func recertificationSourceHash(namespace Namespace, additionalInfo cas.AdditionalInfo) (string, bool) {
	if !additionalInfo.Recertified {
		return "", false
	}
	recordNamespace, recordHash, ok := recordHashFromID(additionalInfo.RecertifiedFromRecord)
	if ok && recordNamespace == namespace {
		return recordHash, true
	}
	if _, _, ok := splitRecordHash(additionalInfo.RecertifiedFromHash); ok {
		return additionalInfo.RecertifiedFromHash, true
	}
	return "", false
}

func recordHashFromID(id string) (Namespace, string, bool) {
	if id == "" || path.Clean(id) != id {
		return "", "", false
	}
	parts := strings.Split(id, "/")
	if len(parts) != 3 || len(parts[1]) != 2 || parts[2] == "" {
		return "", "", false
	}
	namespace := Namespace(parts[0])
	if err := validateNamespace(namespace); err != nil {
		return "", "", false
	}
	hash := parts[1] + parts[2]
	if _, _, ok := splitRecordHash(hash); !ok {
		return "", "", false
	}
	return namespace, hash, true
}

func (db *DB) packageRecordsForPrune(namespace Namespace, pkg *gocode.Package, currentRelPaths []string) []*priorPackageRecord {
	namespaceDir := db.namespaceDir(namespace)
	records := []*priorPackageRecord{}

	_ = filepath.WalkDir(namespaceDir, func(recordPath string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}

		hash, ok := recordHashFromPath(namespaceDir, recordPath)
		if !ok {
			return nil
		}

		record, err := readFullCASRecordFile(recordPath)
		if err != nil || !db.recordPathsMatchPackage(record.AdditionalInfo.Paths, pkg, currentRelPaths) {
			return nil
		}

		candidate := db.packageRecordSummary(hash, record.AdditionalInfo, recordPath)
		records = append(records, &priorPackageRecord{
			summary:    candidate,
			recordPath: recordPath,
		})
		return nil
	})

	sort.Slice(records, func(i, j int) bool {
		return betterPackageRecord(records[i].summary, records[j].summary)
	})
	return records
}

func recordOlderThan(record *PackageRecordSummary, cutoff time.Time) bool {
	return record != nil && !record.Time.IsZero() && record.Time.Before(cutoff)
}

func hasNewerPackageRecord(records []*priorPackageRecord, candidate *priorPackageRecord) bool {
	if candidate == nil || candidate.summary == nil || candidate.summary.Time.IsZero() {
		return false
	}
	for _, record := range records {
		if record == nil || record == candidate || record.summary == nil {
			continue
		}
		if record.summary.Time.After(candidate.summary.Time) {
			return true
		}
	}
	return false
}

func removeCASRecordPath(recordPath string) (bool, error) {
	if err := os.Remove(recordPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("delete CAS record %q: %w", recordPath, err)
	}
	return true, nil
}

func cleanupCASRecordDirs(namespaceDir, recordPath string) {
	_ = os.Remove(filepath.Dir(recordPath))
	_ = os.Remove(namespaceDir)
}

// Delete removes the stored value for (pkg, spec).
//
// Deleting a missing value is a no-op and returns nil.
//
// Delete returns an error only for "real" failures (I/O, CAS delete failures, etc).
func (db *DB) Delete(pkg *gocode.Package, spec NamespaceSpec) error {
	if pkg == nil {
		return errors.New("package is nil")
	}
	namespace, hasher, _, err := db.hasherForValidatedPackageSpec(pkg, spec)
	if err != nil {
		return err
	}

	return db.delete(namespace, hasher)
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

func validateAbsPath(fieldName, p string) error {
	if p == "" {
		return fmt.Errorf("%s is empty", fieldName)
	}
	if !filepath.IsAbs(p) {
		return fmt.Errorf("%s must be an absolute path: %q", fieldName, p)
	}
	return nil
}

func nearestGitRoot(absBaseDir string) (string, error) {
	dir := absBaseDir
	if fi, err := os.Stat(dir); err == nil && !fi.IsDir() {
		dir = filepath.Dir(dir)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("stat baseDir: %w", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("stat .git in %q: %w", dir, err)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no git root found for baseDir %q", absBaseDir)
		}
		dir = parent
	}
}

func validateNamespaceSpec(spec NamespaceSpec) error {
	if spec.Name == "" {
		return errors.New("namespace spec name is empty")
	}
	if err := validateNamespace(Namespace(spec.Name)); err != nil {
		return fmt.Errorf("namespace spec name: %w", err)
	}
	if spec.Version <= 0 {
		return fmt.Errorf("namespace spec version must be positive: %d", spec.Version)
	}
	switch spec.HashMode {
	case HashModePackage, HashModeCodeUnit:
	default:
		return fmt.Errorf("unsupported namespace spec hash mode %q", spec.HashMode)
	}
	if err := validateNamespace(spec.Namespace()); err != nil {
		return err
	}
	return nil
}

func (db *DB) validateCommon(spec NamespaceSpec) error {
	if db == nil {
		return errors.New("gocas DB is nil")
	}
	if err := validateNamespaceSpec(spec); err != nil {
		return err
	}
	if err := validateAbsDir("BaseDir", db.BaseDir); err != nil {
		return err
	}
	if err := validateAbsPath("cas.DB.AbsRoot", db.DB.AbsRoot); err != nil {
		return err
	}
	return nil
}

func (db *DB) hasherForValidatedPackageSpec(pkg *gocode.Package, spec NamespaceSpec) (Namespace, cas.Hasher, []string, error) {
	if err := db.validateCommon(spec); err != nil {
		return "", nil, nil, err
	}

	namespace := spec.Namespace()
	hasher, relPaths, err := db.hasherForPackageSpec(pkg, spec)
	if err != nil {
		return "", nil, nil, err
	}
	return namespace, hasher, relPaths, nil
}

func (db *DB) store(namespace Namespace, hasher cas.Hasher, relPaths []string, jsonable any) error {
	additionalInfo := cas.AdditionalInfo{
		UnixTimestamp: int(time.Now().Unix()),
		Paths:         relPaths,
	}
	db.bestEffortPopulateGitInfo(&additionalInfo)

	return db.DB.Store(hasher, string(namespace), jsonable, &cas.Options{
		AdditionalInfo: additionalInfo,
	})
}

func (db *DB) retrieve(namespace Namespace, hasher cas.Hasher, target any) (ok bool, additionalInfo cas.AdditionalInfo, err error) {
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

func (db *DB) delete(namespace Namespace, hasher cas.Hasher) error {
	return db.DB.Delete(hasher, string(namespace))
}

func (db *DB) namespaceDir(namespace Namespace) string {
	return filepath.Join(db.DB.AbsRoot, string(namespace))
}

func (db *DB) recordPath(namespace Namespace, hash string) (string, bool) {
	prefix, suffix, ok := splitRecordHash(hash)
	if !ok {
		return "", false
	}
	return filepath.Join(db.namespaceDir(namespace), prefix, suffix), true
}

func (db *DB) packageRecordSummary(hash string, additionalInfo cas.AdditionalInfo, recordPath string) *PackageRecordSummary {
	return &PackageRecordSummary{
		Hash:           hash,
		Time:           db.recordTime(additionalInfo, recordPath),
		AdditionalInfo: additionalInfo,
	}
}

func (db *DB) recordTime(additionalInfo cas.AdditionalInfo, recordPath string) time.Time {
	if t, ok := db.recordAddCommitTime(recordPath); ok {
		return t
	}
	return fallbackRecordTime(additionalInfo, recordPath)
}

func (db *DB) recordAddCommitTime(recordPath string) (time.Time, bool) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return time.Time{}, false
	}

	for _, commit := range db.gitCommitsAddingRecord(recordPath, gitPath) {
		t, ok := gitCommitTime(db.BaseDir, gitPath, commit)
		if ok {
			return t, true
		}
	}
	return time.Time{}, false
}

func fallbackRecordTime(additionalInfo cas.AdditionalInfo, recordPath string) time.Time {
	if additionalInfo.UnixTimestamp > 0 {
		return time.Unix(int64(additionalInfo.UnixTimestamp), 0)
	}
	if recordPath == "" {
		return time.Time{}
	}
	fi, err := os.Stat(recordPath)
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

type casRecordFile struct {
	Kind           string             `json:"kind"`
	Metadata       json.RawMessage    `json:"metadata"`
	AdditionalInfo cas.AdditionalInfo `json:"additional_info"`
}

type priorPackageRecord struct {
	summary    *PackageRecordSummary
	recordPath string
}

func readFullCASRecordFile(recordPath string) (casRecordFile, error) {
	b, err := os.ReadFile(recordPath)
	if err != nil {
		return casRecordFile{}, err
	}

	var record casRecordFile
	if err := json.Unmarshal(b, &record); err != nil {
		return casRecordFile{}, fmt.Errorf("%w: %v", errInvalidCASRecord, err)
	}
	if record.Kind != casRecordKind || len(record.Metadata) == 0 {
		return casRecordFile{}, errInvalidCASRecord
	}
	return record, nil
}

func readCASRecordFile(recordPath string) (cas.AdditionalInfo, bool) {
	record, err := readFullCASRecordFile(recordPath)
	if err != nil {
		return cas.AdditionalInfo{}, false
	}
	return record.AdditionalInfo, true
}

func recordID(namespace Namespace, hash string) (string, bool) {
	prefix, suffix, ok := splitRecordHash(hash)
	if !ok {
		return "", false
	}
	return path.Join(string(namespace), prefix, suffix), true
}

func splitRecordHash(hash string) (string, string, bool) {
	if len(hash) < 3 {
		return "", "", false
	}
	return hash[:2], hash[2:], true
}

func (db *DB) recertificationWarnings(currentInfo cas.AdditionalInfo, source *PackageRecordSummary, records []*priorPackageRecord, currentRelPaths []string) []string {
	warnings := []string{}
	if currentInfo.GitCommit != "" && !currentInfo.GitClean {
		warnings = append(warnings, "current git worktree is dirty")
	}
	if churn := db.churnPercent(records, currentRelPaths); churn != nil && *churn >= 20 {
		warnings = append(warnings, "large churn (>=20%)")
	}
	if source != nil && !source.Time.IsZero() && time.Since(source.Time) >= 30*24*time.Hour {
		warnings = append(warnings, "source record is >=30 days old")
	}
	return warnings
}

func (db *DB) priorInvalidatedPackageRecords(namespace Namespace, currentHash string, pkg *gocode.Package, currentRelPaths []string) []*priorPackageRecord {
	namespaceDir := db.namespaceDir(namespace)
	records := []*priorPackageRecord{}

	_ = filepath.WalkDir(namespaceDir, func(recordPath string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}

		hash, ok := recordHashFromPath(namespaceDir, recordPath)
		if !ok || hash == currentHash {
			return nil
		}

		additionalInfo, ok := readCASRecordFile(recordPath)
		if !ok || !db.recordPathsMatchPackage(additionalInfo.Paths, pkg, currentRelPaths) {
			return nil
		}

		candidate := db.packageRecordSummary(hash, additionalInfo, recordPath)
		records = append(records, &priorPackageRecord{
			summary:    candidate,
			recordPath: recordPath,
		})
		return nil
	})

	sort.Slice(records, func(i, j int) bool {
		return betterPackageRecord(records[i].summary, records[j].summary)
	})
	return records
}

func recordHashFromPath(namespaceDir, recordPath string) (string, bool) {
	rel, err := filepath.Rel(namespaceDir, recordPath)
	if err != nil {
		return "", false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) != 2 || len(parts[0]) != 2 || parts[1] == "" {
		return "", false
	}
	return parts[0] + parts[1], true
}

func betterPackageRecord(candidate, incumbent *PackageRecordSummary) bool {
	if candidate == nil {
		return false
	}
	if incumbent == nil {
		return true
	}
	if candidate.Time.After(incumbent.Time) {
		return true
	}
	if incumbent.Time.After(candidate.Time) {
		return false
	}
	return candidate.Hash > incumbent.Hash
}

func (db *DB) recordPathsMatchPackage(paths []string, pkg *gocode.Package, currentRelPaths []string) bool {
	normalizedPaths := db.normalizeStoredPaths(paths)
	if len(normalizedPaths) == 0 {
		return false
	}

	currentPathSet := make(map[string]struct{}, len(currentRelPaths))
	for _, p := range currentRelPaths {
		rel, ok := cleanRelPath(p)
		if ok {
			currentPathSet[rel] = struct{}{}
		}
	}
	for _, p := range normalizedPaths {
		if _, ok := currentPathSet[p]; ok {
			return true
		}
	}

	pkgRelDir, ok := cleanRelPath(pkg.RelativeDir)
	if !ok || pkgRelDir == "." {
		for _, p := range normalizedPaths {
			if !strings.Contains(p, "/") {
				return true
			}
		}
		return false
	}

	prefix := strings.TrimSuffix(pkgRelDir, "/") + "/"
	for _, p := range normalizedPaths {
		if p == pkgRelDir || strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

func (db *DB) normalizeStoredPaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	normalized := make([]string, 0, len(paths))
	for _, p := range paths {
		rel, ok := db.normalizeStoredPath(p)
		if !ok {
			continue
		}
		if _, ok := seen[rel]; ok {
			continue
		}
		seen[rel] = struct{}{}
		normalized = append(normalized, rel)
	}
	sort.Strings(normalized)
	return normalized
}

func (db *DB) normalizeStoredPath(p string) (string, bool) {
	if p == "" {
		return "", false
	}
	if filepath.IsAbs(p) {
		rel, err := filepath.Rel(db.BaseDir, p)
		if err != nil {
			return "", false
		}
		return cleanRelPath(rel)
	}
	return cleanRelPath(p)
}

func cleanRelPath(p string) (string, bool) {
	p = strings.ReplaceAll(p, `\`, "/")
	p = path.Clean(p)
	if p == "" || p == "." || p == ".." || strings.HasPrefix(p, "../") {
		return "", false
	}
	return p, true
}

func (db *DB) churnPercent(records []*priorPackageRecord, currentRelPaths []string) *float64 {
	if len(records) == 0 {
		return nil
	}

	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil
	}

	for _, record := range records {
		churn := db.recordChurnPercent(record, currentRelPaths, gitPath)
		if churn != nil {
			return churn
		}
	}
	return nil
}

func (db *DB) recordChurnPercent(record *priorPackageRecord, currentRelPaths []string, gitPath string) *float64 {
	if record == nil || record.summary == nil {
		return nil
	}

	priorPaths := db.normalizeStoredPaths(record.summary.AdditionalInfo.Paths)
	if len(priorPaths) == 0 {
		return nil
	}

	commit, ok := db.matchingRecordCommit(record, priorPaths, gitPath)
	if !ok {
		return nil
	}

	allPaths := mergeRelPaths(priorPaths, currentRelPaths)
	changedLines, ok := gitChangedLines(db.BaseDir, gitPath, commit, allPaths)
	if !ok {
		return nil
	}

	priorLines, ok := gitCommitLineCount(db.BaseDir, gitPath, commit, priorPaths)
	if !ok || priorLines == 0 {
		return nil
	}

	churn := (float64(changedLines) / float64(priorLines)) * 100
	return &churn
}

func (db *DB) matchingRecordCommit(record *priorPackageRecord, priorPaths []string, gitPath string) (string, bool) {
	for _, commit := range db.recordCommitCandidates(record, gitPath) {
		if db.recordHashMatchesCommit(record.summary.Hash, commit, priorPaths, gitPath) {
			return commit, true
		}
	}
	return "", false
}

func (db *DB) recordCommitCandidates(record *priorPackageRecord, gitPath string) []string {
	if record == nil || record.summary == nil {
		return nil
	}

	seen := map[string]struct{}{}
	candidates := []string{}
	add := func(commit string) {
		commit = strings.TrimSpace(commit)
		if commit == "" {
			return
		}
		if _, ok := seen[commit]; ok {
			return
		}
		seen[commit] = struct{}{}
		candidates = append(candidates, commit)
	}

	add(record.summary.AdditionalInfo.GitCommit)
	for _, commit := range db.gitCommitsAddingRecord(record.recordPath, gitPath) {
		add(commit)
	}
	return candidates
}

func (db *DB) gitCommitsAddingRecord(recordPath string, gitPath string) []string {
	if recordPath == "" {
		return nil
	}

	prefix, err := gitOutput(db.BaseDir, gitPath, "rev-parse", "--show-prefix")
	if err != nil {
		return nil
	}
	relFromBase, err := filepath.Rel(db.BaseDir, recordPath)
	if err != nil {
		return nil
	}
	rel, ok := cleanRelPath(path.Join(filepath.ToSlash(prefix), filepath.ToSlash(relFromBase)))
	if !ok {
		return nil
	}

	out, err := gitOutput(db.BaseDir, gitPath, "log", "--format=%H", "--diff-filter=A", "--", ":(top)"+rel)
	if err != nil {
		return nil
	}

	commits := []string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			commits = append(commits, line)
		}
	}
	return commits
}

func (db *DB) recordHashMatchesCommit(hash, commit string, relPaths []string, gitPath string) bool {
	if hash == "" || commit == "" || len(relPaths) == 0 {
		return false
	}

	tmpDir, err := os.MkdirTemp("", "codalotl-cas-hash-*")
	if err != nil {
		return false
	}
	defer os.RemoveAll(tmpDir)

	absPaths := make([]string, 0, len(relPaths))
	for _, relPath := range relPaths {
		relPath, ok := cleanRelPath(relPath)
		if !ok {
			return false
		}
		out, ok := gitShowFile(db.BaseDir, gitPath, commit, relPath)
		if !ok {
			return false
		}

		absPath := filepath.Join(tmpDir, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return false
		}
		if err := os.WriteFile(absPath, out, 0o644); err != nil {
			return false
		}
		absPaths = append(absPaths, absPath)
	}

	hasher, err := cas.NewDirRelativeFileSetHasher(tmpDir, absPaths)
	if err != nil {
		return false
	}
	return hasher.Hash() == hash
}

func mergeRelPaths(a []string, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	merged := make([]string, 0, len(a)+len(b))
	add := func(p string) {
		rel, ok := cleanRelPath(p)
		if !ok {
			return
		}
		if _, ok := seen[rel]; ok {
			return
		}
		seen[rel] = struct{}{}
		merged = append(merged, rel)
	}
	for _, p := range a {
		add(p)
	}
	for _, p := range b {
		add(p)
	}
	sort.Strings(merged)
	return merged
}

func gitChangedLines(dir, gitPath, commit string, relPaths []string) (int, bool) {
	if len(relPaths) == 0 {
		return 0, false
	}

	args := []string{"diff", "--numstat", "--no-ext-diff", "--no-renames", commit, "--"}
	args = append(args, relPaths...)
	out, err := gitOutput(dir, gitPath, args...)
	if err != nil {
		return 0, false
	}

	changed := 0
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 3 || fields[0] == "-" || fields[1] == "-" {
			continue
		}
		added, err := parseNonNegativeInt(fields[0])
		if err != nil {
			return 0, false
		}
		deleted, err := parseNonNegativeInt(fields[1])
		if err != nil {
			return 0, false
		}
		changed += added + deleted
	}
	untrackedChanged, ok := gitUntrackedChangedLines(dir, gitPath, relPaths)
	if !ok {
		return 0, false
	}
	changed += untrackedChanged
	return changed, true
}

func gitUntrackedChangedLines(dir, gitPath string, relPaths []string) (int, bool) {
	args := []string{"ls-files", "--others", "--exclude-standard", "-z", "--"}
	args = append(args, relPaths...)
	out, err := gitOutputBytes(dir, gitPath, args...)
	if err != nil {
		return 0, false
	}

	changed := 0
	for _, rawPath := range bytes.Split(out, []byte{0}) {
		if len(rawPath) == 0 {
			continue
		}
		relPath, ok := cleanRelPath(string(rawPath))
		if !ok {
			return 0, false
		}
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(relPath)))
		if err != nil {
			return 0, false
		}
		changed += countLines(b)
	}
	return changed, true
}

func gitCommitLineCount(dir, gitPath, commit string, relPaths []string) (int, bool) {
	total := 0
	sawFile := false
	for _, relPath := range relPaths {
		out, ok := gitShowFile(dir, gitPath, commit, relPath)
		if !ok {
			continue
		}
		sawFile = true
		total += countLines(out)
	}
	return total, sawFile
}

func gitCommitTime(dir, gitPath, commit string) (time.Time, bool) {
	commit = strings.TrimSpace(commit)
	if commit == "" {
		return time.Time{}, false
	}

	out, err := gitOutput(dir, gitPath, "show", "-s", "--format=%ct", commit)
	if err != nil {
		return time.Time{}, false
	}
	seconds, err := parseNonNegativeInt(strings.TrimSpace(out))
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(int64(seconds), 0), true
}

func gitShowFile(dir, gitPath, commit, relPath string) ([]byte, bool) {
	objectPath, ok := gitObjectPath(dir, gitPath, relPath)
	if !ok {
		return nil, false
	}

	out, err := gitOutputBytes(dir, gitPath, "show", commit+":"+objectPath)
	if err != nil {
		return nil, false
	}
	return out, true
}

func gitObjectPath(dir, gitPath, relPath string) (string, bool) {
	relPath, ok := cleanRelPath(relPath)
	if !ok {
		return "", false
	}

	prefix, err := gitOutput(dir, gitPath, "rev-parse", "--show-prefix")
	if err != nil {
		return "", false
	}
	if prefix == "" {
		return relPath, true
	}
	return strings.TrimSuffix(prefix, "/") + "/" + relPath, true
}

func gitOutputBytes(dir, gitPath string, args ...string) ([]byte, error) {
	cmd := exec.Command(gitPath, args...)
	cmd.Dir = dir
	return cmd.Output()
}

func countLines(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	n := bytes.Count(b, []byte{'\n'})
	if b[len(b)-1] != '\n' {
		n++
	}
	return n
}

func parseNonNegativeInt(s string) (int, error) {
	if s == "" {
		return 0, errors.New("empty integer")
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("invalid non-negative integer %q", s)
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}

type fileRec struct {
	abs string
	rel string
}

type fileRecOptions struct {
	emptyPathErr    string
	outsideBaseKind string
	allowNotExist   bool
}

func (db *DB) hasherForPackageSpec(pkg *gocode.Package, spec NamespaceSpec) (cas.Hasher, []string, error) {
	switch spec.HashMode {
	case HashModePackage:
		return db.hasherForPackage(pkg)
	case HashModeCodeUnit:
		unit, err := codeunit.DefaultGoCodeUnit(pkg.AbsolutePath())
		if err != nil {
			return nil, nil, err
		}
		return db.hasherForCodeUnit(unit)
	default:
		return nil, nil, fmt.Errorf("unsupported namespace spec hash mode %q", spec.HashMode)
	}
}

func (db *DB) hasherForPackage(pkg *gocode.Package) (cas.Hasher, []string, error) {
	seen := make(map[string]struct{})
	recs := make([]fileRec, 0, len(pkg.Files))
	addAbsPath := func(abs string, allowNotExist bool) error {
		return db.appendFileRec(&recs, seen, abs, fileRecOptions{
			emptyPathErr:    "package file has empty absolute path",
			outsideBaseKind: "package file",
			allowNotExist:   allowNotExist,
		})
	}
	addFiles := func(p *gocode.Package) error {
		if p == nil {
			return nil
		}
		for _, f := range p.Files {
			if f == nil {
				continue
			}
			if err := addAbsPath(f.AbsolutePath, false); err != nil {
				return err
			}
		}
		return nil
	}
	addOptionalSpec := func(p *gocode.Package) error {
		if p == nil {
			return nil
		}
		return addAbsPath(filepath.Join(p.AbsolutePath(), "SPEC.md"), true)
	}

	if err := addFiles(pkg); err != nil {
		return nil, nil, err
	}
	if err := addFiles(pkg.TestPackage); err != nil {
		return nil, nil, err
	}
	if err := addOptionalSpec(pkg); err != nil {
		return nil, nil, err
	}
	if err := addOptionalSpec(pkg.TestPackage); err != nil {
		return nil, nil, err
	}

	return db.hasherForFileRecs(recs)
}

func (db *DB) hasherForCodeUnit(unit *codeunit.CodeUnit) (cas.Hasher, []string, error) {
	seen := make(map[string]struct{})
	recs := make([]fileRec, 0, len(unit.IncludedFiles()))
	for _, abs := range unit.IncludedFiles() {
		err := db.appendFileRec(&recs, seen, abs, fileRecOptions{
			emptyPathErr:    "code unit includes an empty path",
			outsideBaseKind: "included file",
		})
		if err != nil {
			return nil, nil, err
		}
	}

	return db.hasherForFileRecs(recs)
}

func (db *DB) appendFileRec(recs *[]fileRec, seen map[string]struct{}, abs string, opts fileRecOptions) error {
	if abs == "" {
		return errors.New(opts.emptyPathErr)
	}
	if _, ok := seen[abs]; ok {
		return nil
	}
	seen[abs] = struct{}{}

	fi, err := os.Stat(abs)
	if err != nil {
		if opts.allowNotExist && errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if fi.IsDir() {
		return nil
	}

	rel, err := filepath.Rel(db.BaseDir, abs)
	if err != nil {
		return err
	}
	if relPathOutsideBase(rel) {
		return fmt.Errorf("%s %q is outside BaseDir %q", opts.outsideBaseKind, abs, db.BaseDir)
	}
	*recs = append(*recs, fileRec{abs: abs, rel: rel})
	return nil
}

func relPathOutsideBase(rel string) bool {
	return rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) ||
		strings.HasPrefix(rel, "../") ||
		strings.HasPrefix(rel, `..\`)
}

func (db *DB) hasherForFileRecs(recs []fileRec) (cas.Hasher, []string, error) {
	sort.Slice(recs, func(i, j int) bool { return recs[i].rel < recs[j].rel })
	fileAbsPaths := make([]string, 0, len(recs))
	fileRelPaths := make([]string, 0, len(recs))
	for _, r := range recs {
		fileAbsPaths = append(fileAbsPaths, r.abs)
		fileRelPaths = append(fileRelPaths, r.rel)
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
	out, err := gitOutputBytes(dir, gitPath, args...)
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
