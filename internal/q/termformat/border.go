package termformat

type border struct {
	left        rune
	right       rune
	top         rune
	bottom      rune
	topLeft     rune
	topRight    rune
	bottomLeft  rune
	bottomRight rune
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
