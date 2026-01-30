package termformat

import (
	"errors"
	"math"
	"sort"
	"strings"

	"github.com/codalotl/codalotl/internal/q/uni"
)

type LayoutBlock struct {
	Block string
	X     int // 0: left-most. Increasing X -> rightward.
	Y     int // 0: top-most. Increasing Y -> downward.
}

var errLayoutOverlap = errors.New("termformat: blocks overlap")

type normalizedLayoutBlock struct {
	x      int
	y      int
	width  int
	height int
	lines  []string
}

// Layout lays out blocks onto a new string. Each existing block is a block of a given size (it has a height and a width). It will be placed at X, Y.
// Each block will have BlockStylePerLine and BlockNormalizeWidth(Naive) applied to it (if you want another normalization method, just make sure Block is already normalized).
//
// If any block overlaps another block, an error is returned. Callers should handle size and position calculation outside of this function.
//
// If there are gaps, spaces are filled with fillBGColor.
func Layout(blocks []LayoutBlock, fillBGColor Color) (string, error) {
	if len(blocks) == 0 {
		return "", nil
	}

	normalizedBlocks := make([]normalizedLayoutBlock, 0, len(blocks))
	totalWidth := 0
	totalHeight := 0

	for _, blk := range blocks {
		normalized := BlockNormalizeWidth(BlockStylePerLine(blk.Block), BlockNormalizeModeNaive)
		width := BlockWidth(normalized)
		height := BlockHeight(normalized)

		if blk.X+width > totalWidth {
			totalWidth = blk.X + width
		}
		if blk.Y+height > totalHeight {
			totalHeight = blk.Y + height
		}

		if width == 0 || height == 0 {
			continue
		}

		normalizedBlocks = append(normalizedBlocks, normalizedLayoutBlock{
			x:      blk.X,
			y:      blk.Y,
			width:  width,
			height: height,
			lines:  strings.Split(normalized, "\n"),
		})
	}

	for i := 0; i < len(normalizedBlocks); i++ {
		for j := i + 1; j < len(normalizedBlocks); j++ {
			if blocksOverlap(normalizedBlocks[i], normalizedBlocks[j]) {
				return "", errLayoutOverlap
			}
		}
	}

	if totalWidth == 0 || totalHeight == 0 {
		return "", nil
	}

	var fillStyle *Style
	if fillBGColor != nil {
		fillStyle = &Style{
			Background: fillBGColor,
		}
	}

	fillSegment := func(width int) string {
		if width <= 0 {
			return ""
		}
		segment := strings.Repeat(" ", width)
		if fillStyle == nil {
			return segment
		}
		return fillStyle.Wrap(segment)
	}

	lines := make([]string, totalHeight)

	for y := 0; y < totalHeight; y++ {
		rowBlocks := make([]normalizedLayoutBlock, 0, len(normalizedBlocks))
		for _, blk := range normalizedBlocks {
			if y >= blk.y && y < blk.y+blk.height {
				rowBlocks = append(rowBlocks, blk)
			}
		}

		sort.Slice(rowBlocks, func(i, j int) bool {
			return rowBlocks[i].x < rowBlocks[j].x
		})

		var b strings.Builder
		curX := 0

		if len(rowBlocks) == 0 {
			b.WriteString(fillSegment(totalWidth))
		} else {
			for _, blk := range rowBlocks {
				if blk.x > curX {
					b.WriteString(fillSegment(blk.x - curX))
				}

				line := blk.lines[y-blk.y]
				b.WriteString(line)

				curX = blk.x + blk.width
			}

			if curX < totalWidth {
				b.WriteString(fillSegment(totalWidth - curX))
			}
		}

		lines[y] = b.String()
	}

	return strings.Join(lines, "\n"), nil
}

// OverlayRelativePosition locates a box along an axis. 0 is in the middle. -1 places the left or top edge on the left/top border of the background. 1 places the right or bottom edge on the right or bottom edge of the background.
// Values in between those are valid.
type OverlayRelativePosition float64

const (
	OverlayRelativePositionCenter        OverlayRelativePosition = 0.0
	OverlayRelativePositionTopOrLeft     OverlayRelativePosition = -1.0
	OverlayRelativePositionBottomOrRight OverlayRelativePosition = 1.0
)

type OverlayPosition struct {
	AutoX OverlayRelativePosition
	AutoY OverlayRelativePosition

	// If necessary, we can use:
	// Precise *struct {
	// 	X, Y int
	// }
}

// Overlay places blockDialog on top of background (think about a popup dialog being drawn over an arbitrary screen buffer). Both are ANSI-styled blocks with a width and height.
//
// Styles of the background will automatically be managed so that styles "cut off" by the dialog are automatically resumed outside of the dialog, as necessary.
//
// The dialog will be positioned according to pos, defaulting to being compeletely centered.
//
// If blockDialog cannot fit on background, blockDialog will be truncated (top-left of dialog is always present and the last to go, regardless of pos). The resultant block size will always be the same size as background.
func Overlay(blockDialog string, background string, pos OverlayPosition) string {
	bgNormalized := BlockNormalizeWidth(BlockStylePerLine(background), BlockNormalizeModeExtend)
	bgWidth := BlockWidth(bgNormalized)
	bgHeight := BlockHeight(bgNormalized)
	if bgWidth == 0 || bgHeight == 0 {
		return ""
	}

	dialogNormalized := BlockNormalizeWidth(BlockStylePerLine(blockDialog), BlockNormalizeModeExtend)
	dialogWidth := BlockWidth(dialogNormalized)
	dialogHeight := BlockHeight(dialogNormalized)
	if dialogWidth == 0 || dialogHeight == 0 {
		return bgNormalized
	}

	linesBG := strings.Split(bgNormalized, "\n")
	linesDialog := strings.Split(dialogNormalized, "\n")

	positionAlongAxis := func(bgSize, dialogSize int, rel OverlayRelativePosition) int {
		if bgSize <= dialogSize {
			return 0
		}

		space := bgSize - dialogSize
		offset := int(math.Round(float64(space) * (float64(rel) + 1.0) / 2.0))
		if offset < 0 {
			offset = 0
		}
		if offset > space {
			offset = space
		}
		return offset
	}

	startX := positionAlongAxis(bgWidth, dialogWidth, pos.AutoX)
	startY := positionAlongAxis(bgHeight, dialogHeight, pos.AutoY)

	overlayWidth := dialogWidth
	if startX+overlayWidth > bgWidth {
		overlayWidth = bgWidth - startX
	}
	overlayHeight := dialogHeight
	if startY+overlayHeight > bgHeight {
		overlayHeight = bgHeight - startY
	}

	if overlayWidth <= 0 || overlayHeight <= 0 {
		return bgNormalized
	}

	outLines := make([]string, bgHeight)

	for y := 0; y < bgHeight; y++ {
		if y < startY || y >= startY+overlayHeight {
			outLines[y] = linesBG[y]
			continue
		}

		bgLine := linesBG[y]
		dialogLine := linesDialog[y-startY]

		split := splitLineByWidth(bgLine, startX, overlayWidth)

		var b strings.Builder
		b.WriteString(split.prefix)
		activeState := split.startState

		if !activeState.isDefault() {
			b.WriteString(ANSIReset)
		}

		dialogSlice := sliceAndReset(dialogLine, overlayWidth)
		b.WriteString(dialogSlice)
		activeState = defaultState()

		if !split.endState.isDefault() {
			b.WriteString(buildStateTransition(split.endState))
			activeState = split.endState
		}

		b.WriteString(split.suffix)

		activeState = simulateSGRState(activeState, split.suffix)
		if !activeState.isDefault() {
			b.WriteString(ANSIReset)
		}

		outLines[y] = b.String()
	}

	return strings.Join(outLines, "\n")
}

func blocksOverlap(a, b normalizedLayoutBlock) bool {
	return a.x < b.x+b.width &&
		a.x+a.width > b.x &&
		a.y < b.y+b.height &&
		a.y+a.height > b.y
}

type lineSplit struct {
	prefix     string
	middle     string
	suffix     string
	startState state
	endState   state
}

func splitLineByWidth(line string, start, length int) lineSplit {
	var (
		prefixBuilder strings.Builder
		middleBuilder strings.Builder
		suffixBuilder strings.Builder
	)

	areaStart := start
	areaEnd := start + length

	active := defaultState()
	startState := active
	endState := active
	startCaptured := false
	endCaptured := false

	writeTo := func(col int) *strings.Builder {
		switch {
		case col < areaStart:
			return &prefixBuilder
		case col < areaEnd:
			return &middleBuilder
		default:
			return &suffixBuilder
		}
	}

	i := 0
	col := 0

	for i < len(line) {
		if line[i] == '\x1b' {
			seqLen := ansiSequenceLength(line[i:])
			if seqLen == 0 {
				seqLen = 1
			}

			content := line[i : i+seqLen]
			builder := writeTo(col)

			if seqLen > 1 && i+1 < len(line) && line[i+1] == '[' && content[len(content)-1] == 'm' {
				if params, ok := parseSGRParameters(content[2 : len(content)-1]); ok {
					active, _ = applyParams(active, params)
				}
			}

			builder.WriteString(content)
			i += seqLen

			if !startCaptured && col >= areaStart {
				startState = active
				startCaptured = true
			}
			if !endCaptured && col >= areaEnd {
				endState = active
				endCaptured = true
			}
			continue
		}

		nextEsc := strings.IndexByte(line[i:], '\x1b')
		segmentEnd := len(line)
		if nextEsc >= 0 {
			segmentEnd = i + nextEsc
		}
		segment := line[i:segmentEnd]
		iter := uni.NewGraphemeIterator(segment, nil)

		for iter.Next() {
			grapheme := segment[iter.Start():iter.End()]
			width := iter.TextWidth()

			builder := writeTo(col)
			builder.WriteString(grapheme)
			col += width

			if !startCaptured && col >= areaStart {
				startState = active
				startCaptured = true
			}
			if !endCaptured && col >= areaEnd {
				endState = active
				endCaptured = true
			}
		}

		i = segmentEnd
	}

	if !startCaptured {
		startState = active
	}
	if !endCaptured {
		endState = active
	}

	return lineSplit{
		prefix:     prefixBuilder.String(),
		middle:     middleBuilder.String(),
		suffix:     suffixBuilder.String(),
		startState: startState,
		endState:   endState,
	}
}

func sliceAndReset(line string, width int) string {
	split := splitLineByWidth(line, 0, width)

	out := split.middle
	if !split.endState.isDefault() {
		out += ANSIReset
	}

	return out
}
