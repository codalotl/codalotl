package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// HelpOptions controls standalone help rendering.
type HelpOptions struct {
	LeafCommands bool // LeafCommands lists executable leaf descendants instead of direct child commands.
}

func visibleChildren(cmd *Command) []*Command {
	children := cmd.Commands()
	out := make([]*Command, 0, len(children))
	for _, child := range children {
		if child != nil && !child.Hidden {
			out = append(out, child)
		}
	}
	return out
}

// WriteHelp writes generated help for cmd. root supplies the program name and command path.
func WriteHelp(w io.Writer, root, cmd *Command, opts HelpOptions) {
	if root == nil {
		panic("cli: WriteHelp called with nil root")
	}
	if cmd == nil {
		panic("cli: WriteHelp called with nil cmd")
	}

	if desc := commandDescription(cmd); desc != "" {
		fmt.Fprintln(w, desc)
	} else {
		fmt.Fprintln(w, commandDisplayName(root, cmd))
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintf(w, "  %s\n", usageLine(root, cmd))

	if commands := commandsForHelp(cmd, opts); len(commands) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Commands:")
		for _, command := range commands {
			fmt.Fprintln(w, formatCommandHelpLine(root, command))
		}
	}

	flags := flagsForHelp(cmd)
	if len(flags) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Options:")
		for _, fh := range flags {
			fmt.Fprintln(w, formatFlagHelpLine(fh))
		}
	}

	if len(cmd.ArgHelp) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Args:")
		for _, arg := range cmd.ArgHelp {
			fmt.Fprintln(w, formatArgHelpLine(arg))
		}
	}

	if cmd.Example != "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Examples:")
		ex := strings.TrimRight(cmd.Example, "\n")
		for _, line := range strings.Split(ex, "\n") {
			if line == "" {
				fmt.Fprintln(w)
				continue
			}
			fmt.Fprintf(w, "  %s\n", line)
		}
	}
}

func writeHelp(w io.Writer, root, cmd *Command) {
	WriteHelp(w, root, cmd, HelpOptions{})
}

func commandDescription(cmd *Command) string {
	if cmd.Long != "" {
		return strings.TrimRight(cmd.Long, "\n")
	}
	return strings.TrimRight(cmd.Short, "\n")
}

func commandsForHelp(cmd *Command, opts HelpOptions) []*Command {
	if opts.LeafCommands {
		commands := leafCommands(cmd)
		sort.Slice(commands, func(i, j int) bool {
			return commandPathString(commands[i]) < commandPathString(commands[j])
		})
		return commands
	}

	children := visibleChildren(cmd)
	sort.Slice(children, func(i, j int) bool { return children[i].Name < children[j].Name })
	return children
}

func leafCommands(cmd *Command) []*Command {
	var out []*Command
	for _, child := range visibleChildren(cmd) {
		visible := visibleChildren(child)
		if child.Run != nil && len(visible) == 0 {
			out = append(out, child)
			continue
		}
		out = append(out, leafCommands(child)...)
	}
	return out
}

func commandDisplayName(root, cmd *Command) string {
	parts := []string{root.Name}
	if cmd != root {
		path := cmd.pathFromRoot()
		for _, node := range path[1:] {
			parts = append(parts, node.Name)
		}
	}
	return strings.Join(parts, " ")
}

func commandPathString(cmd *Command) string {
	var parts []string
	for _, node := range cmd.pathFromRoot() {
		parts = append(parts, node.Name)
	}
	return strings.Join(parts, " ")
}

func usageLine(root, cmd *Command) string {
	full := commandDisplayName(root, cmd)
	segments := []string{full}
	if options := optionUsageFragment(flagsForHelp(cmd)); options != "" {
		segments = append(segments, options)
	}
	if usage := usageFragment(cmd); usage != "" {
		segments = append(segments, usage)
	}
	return strings.Join(segments, " ")
}

func formatCommandHelpLine(root, cmd *Command) string {
	synopsis := commandSynopsis(root, cmd)
	if cmd.Short == "" {
		return fmt.Sprintf("  %s", synopsis)
	}
	return fmt.Sprintf("  %s\t%s", synopsis, cmd.Short)
}

func commandSynopsis(root, cmd *Command) string {
	segments := []string{commandDisplayName(root, cmd)}
	if options := optionUsageFragment(flagsForHelp(cmd)); options != "" {
		segments = append(segments, options)
	}
	if usage := usageFragment(cmd); usage != "" {
		segments = append(segments, usage)
	}
	return strings.Join(segments, " ")
}

func usageFragment(cmd *Command) string {
	if cmd.Usage != "" {
		return cmd.Usage
	}
	if len(cmd.ArgHelp) > 0 {
		displays := make([]string, 0, len(cmd.ArgHelp))
		for _, arg := range cmd.ArgHelp {
			if arg.Display != "" {
				displays = append(displays, arg.Display)
			}
		}
		if len(displays) > 0 {
			return strings.Join(displays, " ")
		}
	}
	if len(cmd.children) > 0 {
		if cmd.Run == nil {
			return "<command>"
		}
		return "[command]"
	}
	return ""
}

func optionUsageFragment(flags []flagHelp) string {
	if len(flags) == 0 {
		return ""
	}
	if len(flags) > 3 {
		return "[OPTIONS]"
	}

	parts := make([]string, 0, len(flags))
	for _, fh := range flags {
		parts = append(parts, optionUsage(fh))
	}
	return strings.Join(parts, " ")
}

func optionUsage(fh flagHelp) string {
	def := fh.def
	if def.kind == flagBool {
		return fmt.Sprintf("[--%s]", def.name)
	}
	return fmt.Sprintf("[--%s=<%s>]", def.name, strings.ToUpper(fh.kind))
}

func formatFlagHelpLine(fh flagHelp) string {
	def := fh.def
	var names string
	if def.shorthand != 0 {
		names = fmt.Sprintf("-%c, --%s", def.shorthand, def.name)
	} else {
		names = fmt.Sprintf("    --%s", def.name)
	}
	suffix := ""
	if def.kind != flagBool {
		suffix = fmt.Sprintf(" <%s>", fh.kind)
	}
	usage := strings.TrimSpace(def.usage)
	if usage == "" {
		return fmt.Sprintf("  %s%s", names, suffix)
	}
	return fmt.Sprintf("  %s%s\t%s", names, suffix, usage)
}

func formatArgHelpLine(arg ArgHelp) string {
	if arg.Description == "" {
		return fmt.Sprintf("  %s", arg.Display)
	}
	return fmt.Sprintf("  %s\t%s", arg.Display, arg.Description)
}
