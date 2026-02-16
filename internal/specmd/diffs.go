package specmd

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// FormatDiffs formats and writes diffs to out, in a manner that would be helpful to a human or LLM in syncing up the spec and implementation.
func FormatDiffs(diffs []SpecDiff, out io.Writer) error {
	if out == nil {
		return errors.New("specmd: FormatDiffs: nil out")
	}
	if len(diffs) == 0 {
		return nil
	}
	for i, d := range diffs {
		if i != 0 {
			if _, err := io.WriteString(out, "\n"); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(out, "DIFF %d/%d\n", i+1, len(diffs)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "type: %s\n", diffTypeLabel(d.DiffType)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "ids: %s\n", formatIDs(d.IDs)); err != nil {
			return err
		}
		if d.SpecLine > 0 {
			if _, err := fmt.Fprintf(out, "spec: SPEC.md:%d\n", d.SpecLine); err != nil {
				return err
			}
		} else {
			if _, err := io.WriteString(out, "spec: SPEC.md:?\n"); err != nil {
				return err
			}
		}
		if d.ImplFile != "" && d.ImplLine > 0 {
			if _, err := fmt.Fprintf(out, "impl: %s:%d\n", d.ImplFile, d.ImplLine); err != nil {
				return err
			}
		} else if d.ImplFile != "" {
			if _, err := fmt.Fprintf(out, "impl: %s:?\n", d.ImplFile); err != nil {
				return err
			}
		} else {
			if _, err := io.WriteString(out, "impl: <missing>\n"); err != nil {
				return err
			}
		}

		if _, err := io.WriteString(out, "\nSPEC:\n"); err != nil {
			return err
		}
		if err := writeGoFence(out, d.SpecSnippet); err != nil {
			return err
		}
		if _, err := io.WriteString(out, "\nIMPL:\n"); err != nil {
			return err
		}
		if err := writeGoFence(out, d.ImplSnippet); err != nil {
			return err
		}
	}
	return nil
}

func diffTypeLabel(dt DiffType) string {
	switch dt {
	case DiffTypeImplMissing:
		return "impl-missing"
	case DiffTypeIDMismatch:
		return "id-mismatch"
	case DiffTypeCodeMismatch:
		return "code-mismatch"
	case DiffTypeDocMismatch:
		return "doc-mismatch"
	case DiffTypeDocWhitespace:
		return "doc-whitespace"
	case DiffTypeOther:
		fallthrough
	default:
		return "other"
	}
}

func formatIDs(ids []string) string {
	if len(ids) == 0 {
		return "<none>"
	}
	if len(ids) == 1 {
		return ids[0]
	}
	return "[" + strings.Join(ids, ", ") + "]"
}

func writeGoFence(out io.Writer, code string) error {
	if _, err := io.WriteString(out, "```go\n"); err != nil {
		return err
	}
	if code != "" {
		if _, err := io.WriteString(out, code); err != nil {
			return err
		}
		if code[len(code)-1] != '\n' {
			if _, err := io.WriteString(out, "\n"); err != nil {
				return err
			}
		}
	}
	if _, err := io.WriteString(out, "```\n"); err != nil {
		return err
	}
	return nil
}
