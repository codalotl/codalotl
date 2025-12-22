package tui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"unicode/utf8"
)

var (
	pasteStartSeq = []byte{0x1b, '[', '2', '0', '0', '~'}
	pasteEndSeq   = []byte{0x1b, '[', '2', '0', '1', '~'}
)

type controlSequence struct {
	key ControlKey
	alt bool
}

var controlSequenceMap = map[string]controlSequence{
	"\x1b\x7f":   {key: ControlKeyBackspace, alt: true}, // common: Alt-Backspace sends ESC DEL
	"\x1b\x08":   {key: ControlKeyBackspace, alt: true}, // some terminals: Alt-Backspace sends ESC BS
	"\x1b[A":     {key: ControlKeyUp},
	"\x1b[B":     {key: ControlKeyDown},
	"\x1b[C":     {key: ControlKeyRight},
	"\x1b[D":     {key: ControlKeyLeft},
	"\x1b[1;2A":  {key: ControlKeyShiftUp},
	"\x1b[1;2B":  {key: ControlKeyShiftDown},
	"\x1b[1;2C":  {key: ControlKeyShiftRight},
	"\x1b[1;2D":  {key: ControlKeyShiftLeft},
	"\x1b[OA":    {key: ControlKeyShiftUp},    // DECCKM
	"\x1b[OB":    {key: ControlKeyShiftDown},  // DECCKM
	"\x1b[OC":    {key: ControlKeyShiftRight}, // DECCKM
	"\x1b[OD":    {key: ControlKeyShiftLeft},  // DECCKM
	"\x1b[a":     {key: ControlKeyShiftUp},    // urxvt
	"\x1b[b":     {key: ControlKeyShiftDown},  // urxvt
	"\x1b[c":     {key: ControlKeyShiftRight}, // urxvt
	"\x1b[d":     {key: ControlKeyShiftLeft},  // urxvt
	"\x1b[1;3A":  {key: ControlKeyUp, alt: true},
	"\x1b[1;3B":  {key: ControlKeyDown, alt: true},
	"\x1b[1;3C":  {key: ControlKeyRight, alt: true},
	"\x1b[1;3D":  {key: ControlKeyLeft, alt: true},
	"\x1b[1;4A":  {key: ControlKeyShiftUp, alt: true},
	"\x1b[1;4B":  {key: ControlKeyShiftDown, alt: true},
	"\x1b[1;4C":  {key: ControlKeyShiftRight, alt: true},
	"\x1b[1;4D":  {key: ControlKeyShiftLeft, alt: true},
	"\x1b[1;5A":  {key: ControlKeyCtrlUp},
	"\x1b[1;5B":  {key: ControlKeyCtrlDown},
	"\x1b[1;5C":  {key: ControlKeyCtrlRight},
	"\x1b[1;5D":  {key: ControlKeyCtrlLeft},
	"\x1bOH":     {key: ControlKeyHome},
	"\x1bOF":     {key: ControlKeyEnd},
	"\x1b[Oa":    {key: ControlKeyCtrlUp, alt: true},    // urxvt
	"\x1b[Ob":    {key: ControlKeyCtrlDown, alt: true},  // urxvt
	"\x1b[Oc":    {key: ControlKeyCtrlRight, alt: true}, // urxvt
	"\x1b[Od":    {key: ControlKeyCtrlLeft, alt: true},  // urxvt
	"\x1b[1;6A":  {key: ControlKeyCtrlShiftUp},
	"\x1b[1;6B":  {key: ControlKeyCtrlShiftDown},
	"\x1b[1;6C":  {key: ControlKeyCtrlShiftRight},
	"\x1b[1;6D":  {key: ControlKeyCtrlShiftLeft},
	"\x1b[1;7A":  {key: ControlKeyCtrlUp, alt: true},
	"\x1b[1;7B":  {key: ControlKeyCtrlDown, alt: true},
	"\x1b[1;7C":  {key: ControlKeyCtrlRight, alt: true},
	"\x1b[1;7D":  {key: ControlKeyCtrlLeft, alt: true},
	"\x1b[1;8A":  {key: ControlKeyCtrlShiftUp, alt: true},
	"\x1b[1;8B":  {key: ControlKeyCtrlShiftDown, alt: true},
	"\x1b[1;8C":  {key: ControlKeyCtrlShiftRight, alt: true},
	"\x1b[1;8D":  {key: ControlKeyCtrlShiftLeft, alt: true},
	"\x1b[Z":     {key: ControlKeyShiftTab},
	"\x1b[2~":    {key: ControlKeyInsert},
	"\x1b[3;2~":  {key: ControlKeyInsert, alt: true},
	"\x1b[3~":    {key: ControlKeyDelete},
	"\x1b[3;3~":  {key: ControlKeyDelete, alt: true},
	"\x1b[5~":    {key: ControlKeyPgUp},
	"\x1b[5;3~":  {key: ControlKeyPgUp, alt: true},
	"\x1b[5;5~":  {key: ControlKeyCtrlPgUp},
	"\x1b[5^":    {key: ControlKeyCtrlPgUp}, // urxvt
	"\x1b[5;7~":  {key: ControlKeyCtrlPgUp, alt: true},
	"\x1b[6~":    {key: ControlKeyPgDown},
	"\x1b[6;3~":  {key: ControlKeyPgDown, alt: true},
	"\x1b[6;5~":  {key: ControlKeyCtrlPgDown},
	"\x1b[6^":    {key: ControlKeyCtrlPgDown}, // urxvt
	"\x1b[6;7~":  {key: ControlKeyCtrlPgDown, alt: true},
	"\x1b[1~":    {key: ControlKeyHome},
	"\x1b[H":     {key: ControlKeyHome},                     // xterm, lxterm
	"\x1b[1;3H":  {key: ControlKeyHome, alt: true},          // xterm, lxterm
	"\x1b[1;5H":  {key: ControlKeyCtrlHome},                 // xterm, lxterm
	"\x1b[1;7H":  {key: ControlKeyCtrlHome, alt: true},      // xterm, lxterm
	"\x1b[1;2H":  {key: ControlKeyShiftHome},                // xterm, lxterm
	"\x1b[1;4H":  {key: ControlKeyShiftHome, alt: true},     // xterm, lxterm
	"\x1b[1;6H":  {key: ControlKeyCtrlShiftHome},            // xterm, lxterm
	"\x1b[1;8H":  {key: ControlKeyCtrlShiftHome, alt: true}, // xterm, lxterm
	"\x1b[4~":    {key: ControlKeyEnd},
	"\x1b[F":     {key: ControlKeyEnd},                     // xterm, lxterm
	"\x1b[1;3F":  {key: ControlKeyEnd, alt: true},          // xterm, lxterm
	"\x1b[1;5F":  {key: ControlKeyCtrlEnd},                 // xterm, lxterm
	"\x1b[1;7F":  {key: ControlKeyCtrlEnd, alt: true},      // xterm, lxterm
	"\x1b[1;2F":  {key: ControlKeyShiftEnd},                // xterm, lxterm
	"\x1b[1;4F":  {key: ControlKeyShiftEnd, alt: true},     // xterm, lxterm
	"\x1b[1;6F":  {key: ControlKeyCtrlShiftEnd},            // xterm, lxterm
	"\x1b[1;8F":  {key: ControlKeyCtrlShiftEnd, alt: true}, // xterm, lxterm
	"\x1b[7~":    {key: ControlKeyHome},                    // urxvt
	"\x1b[7^":    {key: ControlKeyCtrlHome},                // urxvt
	"\x1b[7$":    {key: ControlKeyShiftHome},               // urxvt
	"\x1b[7@":    {key: ControlKeyCtrlShiftHome},           // urxvt
	"\x1b[8~":    {key: ControlKeyEnd},                     // urxvt
	"\x1b[8^":    {key: ControlKeyCtrlEnd},                 // urxvt
	"\x1b[8$":    {key: ControlKeyShiftEnd},                // urxvt
	"\x1b[8@":    {key: ControlKeyCtrlShiftEnd},            // urxvt
	"\x1b[[A":    {key: ControlKeyF1},                      // linux console
	"\x1b[[B":    {key: ControlKeyF2},                      // linux console
	"\x1b[[C":    {key: ControlKeyF3},                      // linux console
	"\x1b[[D":    {key: ControlKeyF4},                      // linux console
	"\x1b[[E":    {key: ControlKeyF5},                      // linux console
	"\x1bOP":     {key: ControlKeyF1},                      // vt100, xterm
	"\x1bOQ":     {key: ControlKeyF2},                      // vt100, xterm
	"\x1bOR":     {key: ControlKeyF3},                      // vt100, xterm
	"\x1bOS":     {key: ControlKeyF4},                      // vt100, xterm
	"\x1b[1;3P":  {key: ControlKeyF1, alt: true},           // vt100, xterm
	"\x1b[1;3Q":  {key: ControlKeyF2, alt: true},           // vt100, xterm
	"\x1b[1;3R":  {key: ControlKeyF3, alt: true},           // vt100, xterm
	"\x1b[1;3S":  {key: ControlKeyF4, alt: true},           // vt100, xterm
	"\x1b[11~":   {key: ControlKeyF1},                      // urxvt
	"\x1b[12~":   {key: ControlKeyF2},                      // urxvt
	"\x1b[13~":   {key: ControlKeyF3},                      // urxvt
	"\x1b[14~":   {key: ControlKeyF4},                      // urxvt
	"\x1b[15~":   {key: ControlKeyF5},                      // vt100, xterm, also urxvt
	"\x1b[15;3~": {key: ControlKeyF5, alt: true},           // vt100, xterm, also urxvt
	"\x1b[17~":   {key: ControlKeyF6},                      // vt100, xterm, also urxvt
	"\x1b[18~":   {key: ControlKeyF7},                      // vt100, xterm, also urxvt
	"\x1b[19~":   {key: ControlKeyF8},                      // vt100, xterm, also urxvt
	"\x1b[20~":   {key: ControlKeyF9},                      // vt100, xterm, also urxvt
	"\x1b[21~":   {key: ControlKeyF10},                     // vt100, xterm, also urxvt
	"\x1b[17;3~": {key: ControlKeyF6, alt: true},           // vt100, xterm
	"\x1b[18;3~": {key: ControlKeyF7, alt: true},           // vt100, xterm
	"\x1b[19;3~": {key: ControlKeyF8, alt: true},           // vt100, xterm
	"\x1b[20;3~": {key: ControlKeyF9, alt: true},           // vt100, xterm
	"\x1b[21;3~": {key: ControlKeyF10, alt: true},          // vt100, xterm
	"\x1b[23~":   {key: ControlKeyF11},                     // vt100, xterm, also urxvt
	"\x1b[24~":   {key: ControlKeyF12},                     // vt100, xterm, also urxvt
	"\x1b[23;3~": {key: ControlKeyF11, alt: true},          // vt100, xterm
	"\x1b[24;3~": {key: ControlKeyF12, alt: true},          // vt100, xterm
	"\x1b[1;2P":  {key: ControlKeyF13},
	"\x1b[1;2Q":  {key: ControlKeyF14},
	"\x1b[25~":   {key: ControlKeyF13},            // vt100, xterm, also urxvt
	"\x1b[26~":   {key: ControlKeyF14},            // vt100, xterm, also urxvt
	"\x1b[25;3~": {key: ControlKeyF13, alt: true}, // vt100, xterm
	"\x1b[26;3~": {key: ControlKeyF14, alt: true}, // vt100, xterm
	"\x1b[1;2R":  {key: ControlKeyF15},
	"\x1b[1;2S":  {key: ControlKeyF16},
	"\x1b[28~":   {key: ControlKeyF15},            // vt100, xterm, also urxvt
	"\x1b[29~":   {key: ControlKeyF16},            // vt100, xterm, also urxvt
	"\x1b[28;3~": {key: ControlKeyF15, alt: true}, // vt100, xterm
	"\x1b[29;3~": {key: ControlKeyF16, alt: true}, // vt100, xterm
	"\x1b[15;2~": {key: ControlKeyF17},
	"\x1b[17;2~": {key: ControlKeyF18},
	"\x1b[18;2~": {key: ControlKeyF19},
	"\x1b[19;2~": {key: ControlKeyF20},
	"\x1b[31~":   {key: ControlKeyF17},
	"\x1b[32~":   {key: ControlKeyF18},
	"\x1b[33~":   {key: ControlKeyF19},
	"\x1b[34~":   {key: ControlKeyF20},
	"\x1bOA":     {key: ControlKeyUp},    // powershell
	"\x1bOB":     {key: ControlKeyDown},  // powershell
	"\x1bOC":     {key: ControlKeyRight}, // powershell
	"\x1bOD":     {key: ControlKeyLeft},  // powershell
}

var controlSequencePrefixes map[string]struct{}

func init() {
	controlSequencePrefixes = make(map[string]struct{})
	for seq := range controlSequenceMap {
		for i := 1; i < len(seq); i++ {
			controlSequencePrefixes[seq[:i]] = struct{}{}
		}
	}
}

type inputProcessor struct {
	t       *TUI
	reader  io.Reader
	fd      int
	pending []byte

	pasteActive bool
	pasteRunes  []rune
	lastWasCR   bool
}

func newInputProcessor(t *TUI, reader io.Reader) *inputProcessor {
	ip := &inputProcessor{
		t:      t,
		reader: reader,
		fd:     -1,
	}
	if fd, ok := extractFD(reader); ok {
		ip.fd = fd
	}
	return ip
}

func (p *inputProcessor) start() {
	p.t.wg.Add(1)
	go func() {
		defer p.t.wg.Done()
		p.run()
	}()
}

func (p *inputProcessor) run() {
	buf := make([]byte, 1024)

	for {
		select {
		case <-p.t.ctx.Done():
			return
		default:
		}

		n, err := p.read(buf)
		if n > 0 {
			p.append(buf[:n])
		}
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
				return
			}
			select {
			case <-p.t.ctx.Done():
				return
			default:
			}
		}
	}
}

func (p *inputProcessor) append(data []byte) {
	p.pending = append(p.pending, data...)
	p.processPending()
}

func (p *inputProcessor) processPending() {
	for len(p.pending) > 0 {
		if p.pasteActive {
			if p.handlePaste() {
				continue
			}
			break
		}

		if bytes.HasPrefix(p.pending, pasteStartSeq) {
			p.pasteActive = true
			p.pasteRunes = p.pasteRunes[:0]
			p.pending = p.pending[len(pasteStartSeq):]
			p.lastWasCR = false
			continue
		}

		b := p.pending[0]
		if b == 0x1b {
			if p.handleEscape() {
				continue
			}
			break
		}

		if handled := p.handleControl(b); handled {
			p.pending = p.pending[1:]
			continue
		}

		if !utf8.FullRune(p.pending) {
			break
		}
		r, size := utf8.DecodeRune(p.pending)
		if r == utf8.RuneError && size == 1 {
			p.pending = p.pending[1:]
			continue
		}
		if !isPrintableRune(r) {
			p.pending = p.pending[size:]
			continue
		}
		p.lastWasCR = false
		p.emitKey(KeyEvent{ControlKey: ControlKeyNone, Runes: []rune{r}})
		p.pending = p.pending[size:]
	}
}

func (p *inputProcessor) handleControl(b byte) bool {
	if b == '\n' && p.lastWasCR {
		p.lastWasCR = false
		return true
	}
	if b == 0x1b {
		return false
	}
	if b < 0x20 || b == 0x7f {
		var key ControlKey
		switch b {
		case '\t':
			key = ControlKeyTab
		case '\r':
			key = ControlKeyEnter
		case 0x7f:
			key = ControlKeyBackspace
		default:
			key = ControlKey(b)
		}
		p.lastWasCR = b == '\r'
		p.emitKey(KeyEvent{ControlKey: key})
		return true
	}
	p.lastWasCR = false
	return false
}

func (p *inputProcessor) handleEscape() bool {
	if len(p.pending) == 0 {
		return false
	}
	p.lastWasCR = false
	if bytes.HasPrefix(p.pending, pasteEndSeq) {
		// Unexpected paste end without start: treat as escape.
		p.emitKey(KeyEvent{ControlKey: ControlKeyEscape})
		p.pending = p.pending[len(pasteEndSeq):]
		return true
	}
	if len(p.pending) == 1 {
		p.emitKey(KeyEvent{ControlKey: ControlKeyEscape})
		p.pending = p.pending[1:]
		return true
	}
	seq, length, ok, needMore := matchControlSequence(p.pending)
	if needMore {
		return false
	}
	if ok {
		p.pending = p.pending[length:]
		p.emitKey(KeyEvent{ControlKey: seq.key, Alt: seq.alt})
		return true
	}

	if p.pending[1] == '[' {
		handled, consumed, needMore := p.handleCSI(p.pending)
		if needMore {
			return false
		}
		if handled {
			p.pending = p.pending[consumed:]
			return true
		}

		seqLen := csiSequenceLength(p.pending)
		if seqLen == 0 {
			return false
		}
		p.pending = p.pending[seqLen:]
		return true
	}
	if p.pending[1] == 'O' {
		if len(p.pending) < 3 {
			return false
		}
	}
	if !utf8.FullRune(p.pending[1:]) {
		return false
	}
	r, size := utf8.DecodeRune(p.pending[1:])
	if r == utf8.RuneError && size == 1 {
		p.emitKey(KeyEvent{ControlKey: ControlKeyEscape})
		p.pending = p.pending[1:]
		return true
	}
	if !isPrintableRune(r) {
		p.emitKey(KeyEvent{ControlKey: ControlKeyEscape})
		p.pending = p.pending[1:]
		return true
	}
	p.emitKey(KeyEvent{ControlKey: ControlKeyNone, Runes: []rune{r}, Alt: true})
	p.pending = p.pending[1+size:]
	return true
}

func (p *inputProcessor) handlePaste() bool {
	if len(p.pending) >= len(pasteEndSeq) && bytes.HasPrefix(p.pending, pasteEndSeq) {
		p.pending = p.pending[len(pasteEndSeq):]
		p.emitPaste()
		p.pasteActive = false
		return true
	}

	if len(p.pending) == 0 {
		return false
	}

	if len(p.pending) < len(pasteEndSeq) && bytes.HasPrefix(pasteEndSeq, p.pending) {
		return false
	}

	if !utf8.FullRune(p.pending) {
		return false
	}

	r, size := utf8.DecodeRune(p.pending)
	if r == utf8.RuneError && size == 1 {
		p.pending = p.pending[size:]
		return true
	}
	if r == '\r' {
		r = '\n'
	}
	if isAllowedPasteRune(r) {
		p.pasteRunes = append(p.pasteRunes, r)
	}
	p.pending = p.pending[size:]
	return true
}

func (p *inputProcessor) emitPaste() {
	if len(p.pasteRunes) == 0 {
		return
	}
	event := KeyEvent{
		ControlKey: ControlKeyNone,
		Runes:      p.pasteRunes,
		Paste:      true,
	}
	p.emitKey(event)
}

func (p *inputProcessor) emitKey(ev KeyEvent) {
	p.t.Send(ev)
}

func (p *inputProcessor) emitMouse(ev MouseEvent) {
	p.t.Send(ev)
}

func (p *inputProcessor) handleCSI(buf []byte) (handled bool, consumed int, needMore bool) {
	if len(buf) < 2 || buf[0] != 0x1b || buf[1] != '[' {
		return false, 0, false
	}
	if len(buf) < 3 {
		return false, 0, true
	}

	// X10 mouse events: ESC [ M Cb Cx Cy
	if buf[2] == 'M' {
		const x10Len = 6
		if len(buf) < x10Len {
			return false, 0, true
		}
		if ev, ok := parseX10MouseEvent(buf[:x10Len]); ok {
			p.emitMouse(ev)
		}
		return true, x10Len, false
	}

	// SGR mouse events: ESC [ < Cb ; Cx ; Cy (M or m)
	if buf[2] == '<' {
		ev, ok, needMore := parseSGRMouseEvent(buf)
		if needMore {
			return false, 0, true
		}
		if ok {
			// parseSGRMouseEvent only returns ok when it saw a terminator.
			for i := 3; i < len(buf); i++ {
				if buf[i] == 'M' || buf[i] == 'm' {
					p.emitMouse(ev)
					return true, i + 1, false
				}
			}
		}
		// Terminator found but parse failed; consume the CSI sequence if possible.
		for i := 3; i < len(buf); i++ {
			if buf[i] == 'M' || buf[i] == 'm' {
				return true, i + 1, false
			}
		}
		return false, 0, true
	}

	return false, 0, false
}

func csiSequenceLength(buf []byte) int {
	if len(buf) < 2 || buf[0] != 0x1b || buf[1] != '[' {
		return 0
	}
	for i := 2; i < len(buf); i++ {
		b := buf[i]
		if b >= 0x40 && b <= 0x7e {
			return i + 1
		}
	}
	return 0
}

func isPrintableRune(r rune) bool {
	return r >= 0x20 && r != 0x7f
}

func isAllowedPasteRune(r rune) bool {
	if r == '\n' || r == '\t' {
		return true
	}
	return isPrintableRune(r)
}

func matchControlSequence(buf []byte) (controlSequence, int, bool, bool) {
	var zero controlSequence
	for i := 1; i <= len(buf); i++ {
		s := string(buf[:i])
		if seq, ok := controlSequenceMap[s]; ok {
			return seq, i, true, false
		}
		if _, ok := controlSequencePrefixes[s]; !ok {
			return zero, 0, false, false
		}
	}
	if _, ok := controlSequencePrefixes[string(buf)]; ok {
		return zero, 0, false, true
	}
	return zero, 0, false, false
}
