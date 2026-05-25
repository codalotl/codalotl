package cas

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

type customHasher string

func (h customHasher) Hash() string { return string(h) }

func TestNewBytesHasher(t *testing.T) {
	h1 := NewBytesHasher([]byte("hello"))
	h2 := NewBytesHasher([]byte("hello"))
	h3 := NewBytesHasher([]byte("hello!"))

	require.Equal(t, h1.Hash(), h2.Hash())
	require.NotEqual(t, h1.Hash(), h3.Hash())
	require.Len(t, h1.Hash(), 64)
}

func TestNewFileSetHasher_OrderIndependent_PathSensitive(t *testing.T) {
	dir := t.TempDir()

	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	c := filepath.Join(dir, "c.txt")

	require.NoError(t, os.WriteFile(a, []byte("same"), 0o644))
	require.NoError(t, os.WriteFile(b, []byte("same"), 0o644))
	require.NoError(t, os.WriteFile(c, []byte("same"), 0o644))

	hAB, err := NewFileSetHasher([]string{a, b})
	require.NoError(t, err)

	hBA, err := NewFileSetHasher([]string{b, a})
	require.NoError(t, err)

	require.Equal(t, hAB.Hash(), hBA.Hash())

	// File names are part of the identity: same contents under different paths should hash differently.
	hAC, err := NewFileSetHasher([]string{a, c})
	require.NoError(t, err)
	require.NotEqual(t, hAB.Hash(), hAC.Hash())

	require.NoError(t, os.WriteFile(b, []byte("changed"), 0o644))
	hABChanged, err := NewFileSetHasher([]string{a, b})
	require.NoError(t, err)
	require.NotEqual(t, hAB.Hash(), hABChanged.Hash())
}

func TestNewDirRelativeFileSetHasher(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "sub", "a.txt")
	outside := filepath.Join(t.TempDir(), "outside.txt")

	require.NoError(t, os.MkdirAll(filepath.Dir(inside), 0o755))
	require.NoError(t, os.WriteFile(inside, []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(outside, []byte("x"), 0o644))

	_, err := NewDirRelativeFileSetHasher(root, []string{inside, outside})
	require.Error(t, err)

	h1, err := NewDirRelativeFileSetHasher(root, []string{inside})
	require.NoError(t, err)

	h2, err := NewDirRelativeFileSetHasher(filepath.Join(root, "."), []string{inside})
	require.NoError(t, err)

	require.Equal(t, h1.Hash(), h2.Hash())
}

func TestDB_StoreRetrieve(t *testing.T) {
	dbRoot := t.TempDir()
	db := &DB{AbsRoot: dbRoot}

	type payload struct {
		A int    `json:"a"`
		B string `json:"b"`
	}

	ns := "docaudit-1.2"
	h := NewBytesHasher([]byte("content"))

	found, _, err := db.Retrieve(h, ns, new(payload))
	require.NoError(t, err)
	require.False(t, found)

	in := payload{A: 1, B: "two"}
	require.NoError(t, db.Store(h, ns, in, nil))

	var out payload
	found, ai, err := db.Retrieve(h, ns, &out)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, in, out)
	require.Equal(t, AdditionalInfo{}, ai)

	// Assert it stored at AbsRoot/<namespace>/<hash[0:2]>/<hash[2:]>.
	hash := h.Hash()
	_, err = os.Stat(filepath.Join(dbRoot, ns, hash[:2], hash[2:]))
	require.NoError(t, err)
}

func TestDB_StoreRetrieve_AdditionalInfo(t *testing.T) {
	dbRoot := t.TempDir()
	db := &DB{AbsRoot: dbRoot}

	ns := "securityreview-1.0"
	h := NewBytesHasher([]byte("content"))

	opts := &Options{
		AdditionalInfo: AdditionalInfo{
			UnixTimestamp:         123,
			Paths:                 []string{"a.go", "b.go"},
			GitClean:              true,
			GitCommit:             "abc123",
			GitMergeBase:          "def456",
			Recertified:           true,
			RecertifiedFromHash:   "oldhash",
			RecertifiedFromRecord: "securityreview-1.0/ab/cdef",
		},
	}

	require.NoError(t, db.Store(h, ns, map[string]any{"ok": true}, opts))

	b, err := os.ReadFile(db.recordPath(ns, h.Hash()))
	require.NoError(t, err)

	var rec struct {
		AdditionalInfo map[string]any `json:"additional_info"`
	}
	require.NoError(t, json.Unmarshal(b, &rec))
	require.Equal(t, true, rec.AdditionalInfo["recertified"])
	require.Equal(t, "oldhash", rec.AdditionalInfo["recertified_from_hash"])
	require.Equal(t, "securityreview-1.0/ab/cdef", rec.AdditionalInfo["recertified_from_record"])

	var out map[string]any
	found, ai, err := db.Retrieve(h, ns, &out)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, map[string]any{"ok": true}, out)
	require.Equal(t, opts.AdditionalInfo, ai)
}

func TestDB_Retrieve_OldStyleAdditionalInfoDefaultsRecertification(t *testing.T) {
	dbRoot := t.TempDir()
	db := &DB{AbsRoot: dbRoot}

	ns := "securityreview-1.0"
	h := NewBytesHasher([]byte("content"))
	recordPath := db.recordPath(ns, h.Hash())

	require.NoError(t, os.MkdirAll(filepath.Dir(recordPath), 0o755))
	require.NoError(t, os.WriteFile(recordPath, []byte(`{
		"kind": "cas-record-v1",
		"metadata": {"ok": true},
		"additional_info": {
			"unix_timestamp": 123,
			"paths": ["a.go", "b.go"],
			"git_clean": true,
			"git_commit": "abc123",
			"git_merge_base": "def456"
		}
	}`), 0o644))

	var out map[string]any
	found, ai, err := db.Retrieve(h, ns, &out)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, map[string]any{"ok": true}, out)
	require.Equal(t, AdditionalInfo{
		UnixTimestamp: 123,
		Paths:         []string{"a.go", "b.go"},
		GitClean:      true,
		GitCommit:     "abc123",
		GitMergeBase:  "def456",
	}, ai)
	require.False(t, ai.Recertified)
	require.Empty(t, ai.RecertifiedFromHash)
	require.Empty(t, ai.RecertifiedFromRecord)
}

func TestDB_Delete(t *testing.T) {
	dbRoot := t.TempDir()
	db := &DB{AbsRoot: dbRoot}

	ns := "docaudit-1.2"
	h := NewBytesHasher([]byte("content"))

	require.NoError(t, db.Delete(h, ns))

	require.NoError(t, db.Store(h, ns, map[string]any{"ok": true}, nil))

	hash := h.Hash()
	recordPath := filepath.Join(dbRoot, ns, hash[:2], hash[2:])
	_, err := os.Stat(recordPath)
	require.NoError(t, err)

	require.NoError(t, db.Delete(h, ns))

	_, err = os.Stat(recordPath)
	require.ErrorIs(t, err, os.ErrNotExist)

	var out map[string]any
	found, ai, err := db.Retrieve(h, ns, &out)
	require.NoError(t, err)
	require.False(t, found)
	require.Equal(t, AdditionalInfo{}, ai)

	require.NoError(t, db.Delete(h, ns))
}

func TestDB_Store_ValidatesNamespaceAndHash(t *testing.T) {
	dbRoot := t.TempDir()
	db := &DB{AbsRoot: dbRoot}

	badHasher := stringHasher("a/b")
	err := db.Store(badHasher, "ok", map[string]any{"x": 1}, nil)
	require.Error(t, err)

	err = db.Store(NewBytesHasher([]byte("x")), "a/b", map[string]any{"x": 1}, nil)
	require.Error(t, err)
}

func TestDB_RecordOperationsRejectDotSegments(t *testing.T) {
	dbRoot := t.TempDir()
	db := &DB{AbsRoot: dbRoot}
	validHasher := NewBytesHasher([]byte("x"))

	tests := []struct {
		name      string
		namespace string
		hasher    Hasher
	}{
		{
			name:      "namespace dot",
			namespace: ".",
			hasher:    validHasher,
		},
		{
			name:      "namespace dotdot",
			namespace: "..",
			hasher:    validHasher,
		},
		{
			name:      "hash dot",
			namespace: "ok",
			hasher:    customHasher("."),
		},
		{
			name:      "hash dotdot",
			namespace: "ok",
			hasher:    customHasher(".."),
		},
		{
			name:      "hash directory segment dotdot",
			namespace: "ok",
			hasher:    customHasher("..x"),
		},
		{
			name:      "hash record segment dot",
			namespace: "ok",
			hasher:    customHasher("ab."),
		},
		{
			name:      "hash record segment dotdot",
			namespace: "ok",
			hasher:    customHasher("ab.."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Error(t, db.Store(tt.hasher, tt.namespace, map[string]any{"x": 1}, nil))

			var out map[string]any
			found, ai, err := db.Retrieve(tt.hasher, tt.namespace, &out)
			require.Error(t, err)
			require.False(t, found)
			require.Equal(t, AdditionalInfo{}, ai)

			require.Error(t, db.Delete(tt.hasher, tt.namespace))
		})
	}

	entries, err := os.ReadDir(dbRoot)
	require.NoError(t, err)
	require.Empty(t, entries)
}
