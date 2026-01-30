package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

func writeHelp(w io.Writer, root, cmd *Command) {
	full := commandDisplayName(root, cmd)
	if cmd.Short != "" {
		fmt.Fprintf(w, "%s - %s\n", full, cmd.Short)
	} else {
		fmt.Fprintf(w, "%s\n", full)
	}

	if cmd.Long != "" {
		fmt.Fprintf(w, "\n%s\n", strings.TrimRight(cmd.Long, "\n"))
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintf(w, "  %s\n", usageLine(root, cmd))

	if len(cmd.children) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Commands:")
		children := cmd.Commands()
		sort.Slice(children, func(i, j int) bool { return children[i].Name < children[j].Name })
		for _, child := range children {
			if child.Short != "" {
				fmt.Fprintf(w, "  %s\t%s\n", child.Name, child.Short)
			} else {
				fmt.Fprintf(w, "  %s\n", child.Name)
			}
		}
	}

	flags := flagsForHelp(cmd)
	if len(flags) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Flags:")
		for _, fh := range flags {
			fmt.Fprintln(w, formatFlagHelpLine(fh))
		}
	}

	if cmd.Example != "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Example:")
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

func usageLine(root, cmd *Command) string {
	full := commandDisplayName(root, cmd)
	segments := []string{full}
	if len(flagsForHelp(cmd)) > 0 {
		segments = append(segments, "[flags]")
	}
	if len(cmd.children) > 0 {
		if cmd.Run == nil {
			segments = append(segments, "<command>")
		} else {
			segments = append(segments, "[command]")
		}
	}
	if cmd.Run != nil {
		segments = append(segments, "[args]")
	}
	return strings.Join(segments, " ")
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
