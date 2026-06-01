package tuicontrols

import "github.com/codalotl/codalotl/internal/q/tui"

// The keyMapKey type is the comparable form of a tui.KeyEvent used by KeyMap lookups.
type keyMapKey struct {
	controlKey tui.ControlKey // The controlKey field is the key event's control-key value.
	runes      string         // The runes field is the key event's rune payload stored as a string.
	alt        bool           // The alt field is true when the key event included the Alt modifier.
	paste      bool           // The paste field is true when the key event came from pasted input.
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
// A semantic event is represented as a string to minimize ceremony and to allow application-level configuration of key bindings.
type KeyMap struct {
	m map[keyMapKey]string // Mappings store semantic events keyed by normalized key events.
}

// NewKeyMap returns an empty key map ready to use.
func NewKeyMap() *KeyMap {
	return &KeyMap{m: make(map[keyMapKey]string)}
}

// Add adds a mapping from key to semanticEvent. If the same key is added multiple times, the last mapping wins.
func (km *KeyMap) Add(key tui.KeyEvent, semanticEvent string) {
	if km.m == nil {
		km.m = make(map[keyMapKey]string)
	}
	km.m[newKeyMapKey(key)] = semanticEvent
}

// Process maps m to one of the semantic events added in Add. If m is not a key event, or doesn't match a configured mapping, "" is returned.
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
