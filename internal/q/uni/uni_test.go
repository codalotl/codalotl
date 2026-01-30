package uni

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTextWidthDefault(t *testing.T) {
	val := "a̐b世"

	assert.Equal(t, 4, TextWidth(val, nil))
	assert.Equal(t, 4, TextWidth([]byte(val), nil))
}

func TestTextWidthOptions(t *testing.T) {
	star := "a☆"
	eye := "a👁"

	assert.Equal(t, 2, TextWidth(star, nil))

	eastAsian := &Options{EastAsianWidth: true}
	assert.Equal(t, 3, TextWidth(star, eastAsian))
	assert.Equal(t, 2, TextWidth(eye, eastAsian))

	wideEmoji := &Options{
		EastAsianWidth:   true,
		TreatEmojiAsWide: true,
	}
	assert.Equal(t, 3, TextWidth(eye, wideEmoji))
}

func TestRuneWidth(t *testing.T) {
	eastAsian := &Options{EastAsianWidth: true}
	wideEmoji := &Options{
		EastAsianWidth:   true,
		TreatEmojiAsWide: true,
	}

	assert.Equal(t, 1, RuneWidth('a', nil))
	assert.Equal(t, 2, RuneWidth('世', nil))
	assert.Equal(t, 1, RuneWidth('☆', nil))
	assert.Equal(t, 2, RuneWidth('☆', eastAsian))
	assert.Equal(t, 1, RuneWidth('👁', eastAsian))
	assert.Equal(t, 2, RuneWidth('👁', wideEmoji))
}

func TestGraphemeIteratorString(t *testing.T) {
	val := "a̐b世"

	iter := NewGraphemeIterator(val, nil)

	var values []string
	var starts []int
	var ends []int
	var widths []int
	for iter.Next() {
		values = append(values, iter.Value())
		starts = append(starts, iter.Start())
		ends = append(ends, iter.End())
		widths = append(widths, iter.TextWidth())
	}

	assert.Equal(t, []string{"a̐", "b", "世"}, values)
	assert.Equal(t, []int{0, 3, 4}, starts)
	assert.Equal(t, []int{3, 4, 7}, ends)
	assert.Equal(t, []int{1, 1, 2}, widths)
}

func TestGraphemeIteratorBytes(t *testing.T) {
	val := "a̐b世"

	iter := NewGraphemeIterator([]byte(val), nil)

	var values []string
	var starts []int
	var ends []int
	var widths []int
	for iter.Next() {
		values = append(values, string(iter.Value()))
		starts = append(starts, iter.Start())
		ends = append(ends, iter.End())
		widths = append(widths, iter.TextWidth())
	}

	assert.Equal(t, []string{"a̐", "b", "世"}, values)
	assert.Equal(t, []int{0, 3, 4}, starts)
	assert.Equal(t, []int{3, 4, 7}, ends)
	assert.Equal(t, []int{1, 1, 2}, widths)
}

func TestIteratorTextWidthOptions(t *testing.T) {
	val := "👁"

	iter := NewGraphemeIterator(val, &Options{EastAsianWidth: true})
	assert.True(t, iter.Next())
	assert.Equal(t, 1, iter.TextWidth())

	iter = NewGraphemeIterator(val, &Options{
		EastAsianWidth:   true,
		TreatEmojiAsWide: true,
	})
	assert.True(t, iter.Next())
	assert.Equal(t, 2, iter.TextWidth())
}
