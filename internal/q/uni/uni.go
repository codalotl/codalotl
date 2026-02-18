package uni

import (
	"github.com/clipperhouse/uax29/v2/graphemes"
	"github.com/mattn/go-runewidth"
)

// Options control width calculations.
//
// EastAsianWidth toggles ambiguous-width characters, and TreatEmojiAsWide only applies when EastAsianWidth is true.
type Options struct {
	EastAsianWidth   bool
	TreatEmojiAsWide bool
}

// TextWidth returns the monospace width of str. If opts is nil, locale is assumed to be non-East Asian.
func TextWidth[T string | []byte](str T, opts *Options) int {
	cond := conditionFromOptions(opts)
	return textWidth(str, cond)
}

// RuneWidth returns the monospace width of r. If opts is nil, locale is assumed to be non-East Asian.
func RuneWidth(r rune, opts *Options) int {
	cond := conditionFromOptions(opts)
	return cond.RuneWidth(rune(r))
}

// Iterator iterates over grapheme clusters.
type Iterator[T string | []byte] struct {
	iter *graphemes.Iterator[T]
	cond *runewidth.Condition
}

// NewGraphemeIterator returns a new grapheme iterator for str. If opts is nil, locale is assumed to be non-East Asian.
func NewGraphemeIterator[T string | []byte](str T, opts *Options) *Iterator[T] {
	cond := conditionFromOptions(opts)
	return &Iterator[T]{
		iter: newGraphemeIterator(str),
		cond: cond,
	}
}

func (iter *Iterator[T]) Next() bool {
	return iter.iter.Next()
}

func (iter *Iterator[T]) Value() T {
	return iter.iter.Value()
}

// Start returns the byte position of the current token in the original data.
func (iter *Iterator[T]) Start() int {
	return iter.iter.Start()
}

// End returns the byte position after the current token in the original data.
func (iter *Iterator[T]) End() int {
	return iter.iter.End()
}

// TextWidth returns the monospace width of the current value.
func (iter *Iterator[T]) TextWidth() int {
	return textWidth(iter.iter.Value(), iter.cond)
}

func conditionFromOptions(opts *Options) *runewidth.Condition {
	cond := runewidth.NewCondition()
	cond.EastAsianWidth = false
	cond.StrictEmojiNeutral = true

	if opts == nil {
		return cond
	}

	cond.EastAsianWidth = opts.EastAsianWidth
	if opts.EastAsianWidth && opts.TreatEmojiAsWide {
		cond.StrictEmojiNeutral = false
	}

	return cond
}

func newGraphemeIterator[T string | []byte](text T) *graphemes.Iterator[T] {
	switch v := any(text).(type) {
	case string:
		iter := graphemes.FromString(v)
		return any(&iter).(*graphemes.Iterator[T])
	case []byte:
		iter := graphemes.FromBytes(v)
		return any(&iter).(*graphemes.Iterator[T])
	default:
		panic("unsupported type")
	}
}

func textWidth[T string | []byte](text T, cond *runewidth.Condition) int {
	switch v := any(text).(type) {
	case string:
		return cond.StringWidth(v)
	case []byte:
		return cond.StringWidth(string(v))
	default:
		panic("unsupported type")
	}
}
