package cli

// RunFunc is a command handler.
type RunFunc func(c *Context) error

// ArgsFunc validates positional args. It should return a UsageError (or any
// ExitCoder with code 2) for user-facing usage mistakes.
type ArgsFunc func(args []string) error

// Command defines one CLI command in a command tree.
type Command struct {
	// Name is the token used to invoke this command (e.g. "add" in "doc add").
	Name string

	// Aliases are additional tokens that invoke this command.
	Aliases []string

	Short   string
	Long    string
	Example string

	Args ArgsFunc // optional
	Run  RunFunc  // optional

	parent          *Command
	children        []*Command
	localFlags      *FlagSet
	persistentFlags *FlagSet
}

// AddCommand adds child commands under c.
func (c *Command) AddCommand(children ...*Command) {
	for _, child := range children {
		if child == nil {
			panic("cli: AddCommand called with nil child")
		}
		if child.parent != nil {
			panic("cli: AddCommand called with a child already attached to a parent")
		}
		if child.Name == "" {
			panic("cli: AddCommand called with a child with empty Name")
		}
		c.children = append(c.children, child)
		child.parent = c
	}
}

// Commands returns the direct children of c.
func (c *Command) Commands() []*Command {
	out := make([]*Command, len(c.children))
	copy(out, c.children)
	return out
}

// Flags returns c's local flags.
func (c *Command) Flags() *FlagSet {
	if c.localFlags == nil {
		c.localFlags = newFlagSet()
	}
	return c.localFlags
}

// PersistentFlags returns flags inherited by c and its descendants.
func (c *Command) PersistentFlags() *FlagSet {
	if c.persistentFlags == nil {
		c.persistentFlags = newFlagSet()
	}
	return c.persistentFlags
}

func (c *Command) childByToken(token string) *Command {
	for _, child := range c.children {
		if child.Name == token {
			return child
		}
		for _, alias := range child.Aliases {
			if alias == token {
				return child
			}
		}
	}
	return nil
}

func (c *Command) pathFromRoot() []*Command {
	var reversed []*Command
	for cur := c; cur != nil; cur = cur.parent {
		reversed = append(reversed, cur)
	}
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	return reversed
}
