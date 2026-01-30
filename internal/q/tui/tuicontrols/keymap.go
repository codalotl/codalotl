package tuicontrols

import "github.com/codalotl/codalotl/internal/q/tui"

type keyMapKey struct {
	controlKey tui.ControlKey
	runes      string
	alt        bool
	paste      bool
}

func newKeyMapKey(k tui.KeyEvent) keyMapKey {
	return keyMapKey{
		controlKey: k.ControlKey,
		runes:      string(k.Runes),
		alt:        k.Alt,
		paste:      k.Paste,
	}
}

// KeyMap maps key events to application-defined semantic events.
//
// A semantic event is represented as a string to minimize ceremony and to allow
// application-level configuration of key bindings.
type KeyMap struct {
	m map[keyMapKey]string
}

func NewKeyMap() *KeyMap {
	return &KeyMap{m: make(map[keyMapKey]string)}
}

// Add adds a mapping from key to semanticEvent. If the same key is added
// multiple times, the last mapping wins.
func (km *KeyMap) Add(key tui.KeyEvent, semanticEvent string) {
	if km.m == nil {
		km.m = make(map[keyMapKey]string)
	}
	km.m[newKeyMapKey(key)] = semanticEvent
}

// Process maps m to one of the semantic events added in Add.
// If m is not a key event, or doesn't match a configured mapping, "" is returned.
func (km *KeyMap) Process(m tui.Message) string {
	key, ok := m.(tui.KeyEvent)
	if !ok {
		return ""
	}
	if km == nil || km.m == nil {
		return ""
	}
	return km.m[newKeyMapKey(key)]
}
