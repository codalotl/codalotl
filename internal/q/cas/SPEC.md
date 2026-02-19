# cas

CAS is a content addressable storage system. It can store metadata against one or more files, or against arbitrary bytes. It can then load that metadata, if the underlying files/bytes haven't changed.

It stores this in a filesystem-backed DB rooted at some configurable path.

## Use cases

This system was originally designed to metadata against Go packages. For example: imagine an LLM that examines a Go package and calculates a security review. We can then store the results of the review, and retrieve it later. If the packages changes, the security review would need to be recalculated.

## Public API

```go

type Blob struct {

}

func (b *Blob) Hash() string

type Hasher interface {
    Hash() string
}


func NewBlob(filePaths []string) (*Blob, error)

func NewBlobFromBytes(b []byte) *Blob

func DB struct {
    AbsRoot string
}


func IsNotFound(err error) bool

type Options struct {
    GitClean bool // Was this stored when the git worktree was clean?
    GitCommit string // Git commit the metadata was calculated against.
    GitMergeBase string // If the commit was on a branch, git commit that was branched off of
}


// NOTE: Options is optional.
func (db *DB) Store(hasher Hasher, namespace string, jsonable any, opts *Options) error

func (db *DB) Retrieve(hasher Hasher, namespace string, target any) error
```