package cli

import (
	"fmt"
	"sort"
	"strconv"
	"time"
)

// Flag kind identifies the parser and destination pointer type for a registered flag.
type flagKind uint8

// Flag kinds identify the supported flag value types.
const (
	flagBool     flagKind = iota + 1 // Boolean flags parse bool values and may omit an explicit value.
	flagString                       // String flags store raw string values.
	flagInt                          // Integer flags parse decimal int values.
	flagDuration                     // Duration flags parse time.Duration values such as "5s".
)

// FlagSet is a typed flag registry for a command.
type FlagSet struct {
	byLong  map[string]*flagDef // Long-name index maps names without "--" to definitions.
	byShort map[rune]*flagDef   // Short-name index maps shorthand runes without "-" to definitions.
}

// flagDef stores the parser metadata and destination pointer for one registered flag.
type flagDef struct {
	name        string         // Name is the long flag name without the leading "--".
	shorthand   rune           // Shorthand is the optional short flag rune without the leading "-"; zero means none.
	usage       string         // Usage is the help text for the flag.
	kind        flagKind       // Kind selects how raw values are parsed and which destination pointer is used.
	boolPtr     *bool          // BoolPtr receives parsed values when Kind is flagBool.
	stringPtr   *string        // StringPtr receives parsed values when Kind is flagString.
	intPtr      *int           // IntPtr receives parsed values when Kind is flagInt.
	durationPtr *time.Duration // DurationPtr receives parsed values when Kind is flagDuration.
}

func newFlagSet() *FlagSet {
	return &FlagSet{
		byLong:  map[string]*flagDef{},
		byShort: map[rune]*flagDef{},
	}
}

// Bool registers a bool flag and returns the storage updated during parsing.
func (fs *FlagSet) Bool(name string, shorthand rune, def bool, usage string) *bool {
	if name == "" {
		panic("cli: flag name must be non-empty")
	}
	ptr := new(bool)
	*ptr = def
	fs.add(&flagDef{
		name:      name,
		shorthand: shorthand,
		usage:     usage,
		kind:      flagBool,
		boolPtr:   ptr,
	})
	return ptr
}

// String registers a string flag and returns the storage updated during parsing.
func (fs *FlagSet) String(name string, shorthand rune, def string, usage string) *string {
	if name == "" {
		panic("cli: flag name must be non-empty")
	}
	ptr := new(string)
	*ptr = def
	fs.add(&flagDef{
		name:      name,
		shorthand: shorthand,
		usage:     usage,
		kind:      flagString,
		stringPtr: ptr,
	})
	return ptr
}

// Int registers an int flag and returns the storage updated during parsing.
func (fs *FlagSet) Int(name string, shorthand rune, def int, usage string) *int {
	if name == "" {
		panic("cli: flag name must be non-empty")
	}
	ptr := new(int)
	*ptr = def
	fs.add(&flagDef{
		name:      name,
		shorthand: shorthand,
		usage:     usage,
		kind:      flagInt,
		intPtr:    ptr,
	})
	return ptr
}

// Duration registers a time.Duration flag and returns the storage updated during parsing.
func (fs *FlagSet) Duration(name string, shorthand rune, def time.Duration, usage string) *time.Duration {
	if name == "" {
		panic("cli: flag name must be non-empty")
	}
	ptr := new(time.Duration)
	*ptr = def
	fs.add(&flagDef{
		name:        name,
		shorthand:   shorthand,
		usage:       usage,
		kind:        flagDuration,
		durationPtr: ptr,
	})
	return ptr
}

// Add registers def in fs's long-name and optional shorthand indexes.
func (fs *FlagSet) add(def *flagDef) {
	if _, ok := fs.byLong[def.name]; ok {
		panic("cli: duplicate flag: --" + def.name)
	}
	fs.byLong[def.name] = def
	if def.shorthand != 0 {
		if _, ok := fs.byShort[def.shorthand]; ok {
			panic(fmt.Sprintf("cli: duplicate shorthand flag: -%c", def.shorthand))
		}
		fs.byShort[def.shorthand] = def
	}
}

// activeFlags is the merged set of flags accepted at the current point in command resolution.
type activeFlags struct {
	byLong  map[string]*flagDef // Long-name index maps names without "--" to active definitions.
	byShort map[rune]*flagDef   // Short-name index maps shorthand runes without "-" to active definitions.
}

// ActiveFlags returns the flags accepted for c: inherited persistent flags plus c's local flags.
func (c *Command) activeFlags() activeFlags {
	byLong := map[string]*flagDef{}
	byShort := map[rune]*flagDef{}

	for _, cmd := range c.pathFromRoot() {
		if cmd.persistentFlags != nil {
			for _, def := range cmd.persistentFlags.byLong {
				addActiveFlag(byLong, byShort, def)
			}
		}
	}

	if c.localFlags != nil {
		for name, def := range c.localFlags.byLong {
			_ = name
			addActiveFlag(byLong, byShort, def)
		}
	}

	return activeFlags{byLong: byLong, byShort: byShort}
}

func addActiveFlag(byLong map[string]*flagDef, byShort map[rune]*flagDef, def *flagDef) {
	if existing, ok := byLong[def.name]; ok && existing != def {
		panic("cli: flag name conflict across command path: --" + def.name)
	}
	byLong[def.name] = def
	if def.shorthand != 0 {
		if existing, ok := byShort[def.shorthand]; ok && existing != def {
			panic(fmt.Sprintf("cli: shorthand conflict across command path: -%c", def.shorthand))
		}
		byShort[def.shorthand] = def
	}
}

// flagHelp is the presentation model for a flag in generated help.
type flagHelp struct {
	def  *flagDef // Def is the flag definition to display.
	kind string   // Kind is the lower-case value type shown for the flag.
}

// flagsForHelp returns cmd's active flags in deterministic help order, excluding the reserved help flag.
func flagsForHelp(cmd *Command) []flagHelp {
	active := cmd.activeFlags()
	var helps []flagHelp
	for _, def := range active.byLong {
		if def.name == "help" || def.shorthand == 'h' {
			continue
		}
		kind := ""
		switch def.kind {
		case flagBool:
			kind = "bool"
		case flagString:
			kind = "string"
		case flagInt:
			kind = "int"
		case flagDuration:
			kind = "duration"
		}
		helps = append(helps, flagHelp{def: def, kind: kind})
	}
	sort.Slice(helps, func(i, j int) bool { return helps[i].def.name < helps[j].def.name })
	return helps
}

// ParseAndSet looks up a flag, parses its value, stores it, and reports whether nextValue was consumed.
func (a activeFlags) parseAndSet(token string, hasDashDash bool, name string, shorthand rune, value *string, nextValue *string) (bool, error) {
	var def *flagDef
	if name != "" {
		def = a.byLong[name]
	} else {
		def = a.byShort[shorthand]
	}
	if def == nil {
		return false, usageErrorf("unknown flag: %s", token)
	}

	consumeNext := false
	var raw string
	if value != nil {
		raw = *value
	} else {
		if def.kind == flagBool {
			if nextValue != nil {
				if _, err := strconv.ParseBool(*nextValue); err == nil {
					raw = *nextValue
					consumeNext = true
				} else {
					raw = "true"
				}
			} else {
				raw = "true"
			}
		} else {
			if nextValue == nil {
				if hasDashDash {
					return false, usageErrorf("flag needs a value before --: %s", token)
				}
				return false, usageErrorf("flag needs a value: %s", token)
			}
			raw = *nextValue
			consumeNext = true
		}
	}

	if err := setFlagValue(def, raw); err != nil {
		return false, usageErrorf("invalid value for %s: %v", displayFlag(def), err)
	}
	return consumeNext, nil
}

// setFlagValue parses raw according to def's kind and stores it in the registered destination pointer.
func setFlagValue(def *flagDef, raw string) error {
	switch def.kind {
	case flagBool:
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return err
		}
		*def.boolPtr = v
		return nil
	case flagString:
		*def.stringPtr = raw
		return nil
	case flagInt:
		v, err := strconv.Atoi(raw)
		if err != nil {
			return err
		}
		*def.intPtr = v
		return nil
	case flagDuration:
		v, err := time.ParseDuration(raw)
		if err != nil {
			return err
		}
		*def.durationPtr = v
		return nil
	default:
		return fmt.Errorf("unknown flag kind")
	}
}

func displayFlag(def *flagDef) string {
	if def.shorthand != 0 {
		return fmt.Sprintf("-%c/--%s", def.shorthand, def.name)
	}
	return "--" + def.name
}
