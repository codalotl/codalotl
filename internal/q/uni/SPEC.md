# uni

The uni package has functions to deal with Unicode:
- Monospace font string width calculations (especially for terminals)
- Grapheme segmentation
- Word segmentation - **planned for future - not implemented now**
- Sentence segmentation - **planned for future - not implemented now**

Over time, if we need to add Unicode helpers, we can add them here.

## Dependencies

Currently, we use github.com/mattn/go-runewidth and github.com/clipperhouse/uax29. This package mostly wraps these packages. The goal with wrapping dependencies is to control the API, so you can protect against breaking changes, swap backends, or bring them in-house.

## Public API

```go
// TextWidth returns the text width of str for monospace fonts in terminals. If opts is nil, locale is assumed to be non-East Asian.
func TextWidth[T string | []byte](str T, opts *Options) int
```

```go
// RuneWidth returns the width of r for monospace fonts in terminals. If opts is nil, locale is assumed to be non-East Asian.
func RuneWidth(r rune, opts *Options) int
```

```go
type Iterator[T string | []byte] struct {
	// ...
}

func (iter *Iterator[T]) Next() bool

func (iter *Iterator[T]) Value() T

// Start returns the byte position of the current token in the original data.
func (iter *Iterator[T]) Start() int

// End returns the byte position after the current token in the original data. Allows looping over bytes [Start(), End()).
func (iter *Iterator[T]) End() int

// TextWidth returns the text width of the current value for monospace fonts in terminals.
func (iter *Iterator[T]) TextWidth() int
```

```go
// Options control width calculation in NewGraphemeIterator and other iterators.
//
// Currently only relevant for East Asian code points and their locale.
type Options struct {
	EastAsianWidth   bool // if true, treats certain East Asian code points as 2 wide (e.g., Chinese, Japanese, Korean). Use if the locale is one of CJK.
	TreatEmojiAsWide bool // Only considered if EastAsianWidth. If true, treats emoji as wide (2 columns).
}
```

```go
// NewGraphemeIterator returns a new grapheme iterator for str (string or []byte). If opts is nil, locale is assumed to be non-East Asian.
func NewGraphemeIterator[T string | []byte](str T, opts *Options) *Iterator[T]
```
