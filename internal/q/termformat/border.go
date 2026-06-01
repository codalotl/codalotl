package termformat

// border contains the runes used to draw a rectangular border.
type border struct {
	left        rune // Left is the rune for the left vertical edge.
	right       rune // Right is the rune for the right vertical edge.
	top         rune // Top is the rune for the top horizontal edge.
	bottom      rune // Bottom is the rune for the bottom horizontal edge.
	topLeft     rune // TopLeft is the rune for the upper-left corner.
	topRight    rune // TopRight is the rune for the upper-right corner.
	bottomLeft  rune // BottomLeft is the rune for the lower-left corner.
	bottomRight rune // BottomRight is the rune for the lower-right corner.
}

var borderNormal = border{
	top:         '─',
	bottom:      '─',
	left:        '│',
	right:       '│',
	topLeft:     '┌',
	topRight:    '┐',
	bottomLeft:  '└',
	bottomRight: '┘',
}

var innerHalfBlockBorder = border{
	top:         '▄',
	bottom:      '▀',
	left:        '▐',
	right:       '▌',
	topLeft:     '▗',
	topRight:    '▖',
	bottomLeft:  '▝',
	bottomRight: '▘',
}

var outerHalfBlockBorder = border{
	top:         '▀',
	bottom:      '▄',
	left:        '▌',
	right:       '▐',
	topLeft:     '▛',
	topRight:    '▜',
	bottomLeft:  '▙',
	bottomRight: '▟',
}

var thickBorder = border{
	top:         '━',
	bottom:      '━',
	left:        '┃',
	right:       '┃',
	topLeft:     '┏',
	topRight:    '┓',
	bottomLeft:  '┗',
	bottomRight: '┛',
}

var hiddenBorder = border{
	top:         ' ',
	bottom:      ' ',
	left:        ' ',
	right:       ' ',
	topLeft:     ' ',
	topRight:    ' ',
	bottomLeft:  ' ',
	bottomRight: ' ',
}
