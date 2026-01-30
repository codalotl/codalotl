package diff

import (
	"fmt"
	"strings"
)

// validate checks the Diff invariants and returns an error on the first violation.
func (d Diff) validate() error {
	var oldConcat, newConcat strings.Builder
	for hi, h := range d.Hunks {
		switch h.Op {
		case OpEqual:
			if h.OldText != h.NewText {
				return fmt.Errorf("hunk[%d]: OpEqual requires OldText==NewText", hi)
			}
			if h.Lines != nil {
				return fmt.Errorf("hunk[%d]: OpEqual requires Lines==nil", hi)
			}
		case OpInsert:
			if h.OldText != "" || h.NewText == "" {
				return fmt.Errorf("hunk[%d]: OpInsert requires OldText==\"\" and NewText!=\"\"", hi)
			}
		case OpDelete:
			if h.OldText == "" || h.NewText != "" {
				return fmt.Errorf("hunk[%d]: OpDelete requires OldText!=\"\" and NewText==\"\"", hi)
			}
		case OpReplace:
			if h.OldText == "" || h.NewText == "" {
				return fmt.Errorf("hunk[%d]: OpReplace requires OldText!=\"\" and NewText!=\"\"", hi)
			}
		}

		oldConcat.WriteString(h.OldText)
		newConcat.WriteString(h.NewText)

		if h.Op == OpEqual {
			continue
		}

		var oldLinesConcat, newLinesConcat strings.Builder
		for li, ln := range h.Lines {
			switch ln.Op {
			case OpEqual:
				if ln.OldText != ln.NewText {
					return fmt.Errorf("hunk[%d].line[%d]: OpEqual requires OldText==NewText", hi, li)
				}
				if ln.Spans != nil {
					return fmt.Errorf("hunk[%d].line[%d]: OpEqual requires Spans==nil", hi, li)
				}
			case OpInsert:
				if ln.OldText != "" || ln.NewText == "" {
					return fmt.Errorf("hunk[%d].line[%d]: OpInsert requires OldText==\"\" and NewText!=\"\"", hi, li)
				}
			case OpDelete:
				if ln.OldText == "" || ln.NewText != "" {
					return fmt.Errorf("hunk[%d].line[%d]: OpDelete requires OldText!=\"\" and NewText==\"\"", hi, li)
				}
			case OpReplace:
				if ln.OldText == "" || ln.NewText == "" {
					return fmt.Errorf("hunk[%d].line[%d]: OpReplace requires OldText!=\"\" and NewText!=\"\"", hi, li)
				}
			}

			oldLinesConcat.WriteString(ln.OldText)
			newLinesConcat.WriteString(ln.NewText)

			if ln.Op == OpEqual {
				continue
			}

			var sOld, sNew strings.Builder
			for si, sp := range ln.Spans {
				if strings.Contains(sp.OldText, defaultEOL) {
					return fmt.Errorf("hunk[%d].line[%d].span[%d]: OldText contains EOL", hi, li, si)
				}
				if strings.Contains(sp.NewText, defaultEOL) {
					return fmt.Errorf("hunk[%d].line[%d].span[%d]: NewText contains EOL", hi, li, si)
				}

				switch sp.Op {
				case OpEqual:
					if sp.OldText != sp.NewText {
						return fmt.Errorf("hunk[%d].line[%d].span[%d]: OpEqual requires OldText==NewText", hi, li, si)
					}
				case OpInsert:
					if sp.OldText != "" || sp.NewText == "" {
						return fmt.Errorf("hunk[%d].line[%d].span[%d]: OpInsert requires OldText==\"\" and NewText!=\"\"", hi, li, si)
					}
				case OpDelete:
					if sp.OldText == "" || sp.NewText != "" {
						return fmt.Errorf("hunk[%d].line[%d].span[%d]: OpDelete requires OldText!=\"\" and NewText==\"\"", hi, li, si)
					}
				case OpReplace:
					if sp.OldText == "" || sp.NewText == "" {
						return fmt.Errorf("hunk[%d].line[%d].span[%d]: OpReplace requires OldText!=\"\" and NewText!=\"\"", hi, li, si)
					}
				}

				sOld.WriteString(sp.OldText)
				sNew.WriteString(sp.NewText)
			}

			oldSuffix := ""
			newSuffix := ""
			if strings.HasSuffix(ln.OldText, defaultEOL) {
				oldSuffix = defaultEOL
			}
			if strings.HasSuffix(ln.NewText, defaultEOL) {
				newSuffix = defaultEOL
			}

			if ln.OldText != sOld.String()+oldSuffix {
				return fmt.Errorf("hunk[%d].line[%d]: spans do not reconstruct OldText", hi, li)
			}
			if ln.NewText != sNew.String()+newSuffix {
				return fmt.Errorf("hunk[%d].line[%d]: spans do not reconstruct NewText", hi, li)
			}
		}

		if h.OldText != oldLinesConcat.String() {
			return fmt.Errorf("hunk[%d]: lines do not reconstruct OldText", hi)
		}
		if h.NewText != newLinesConcat.String() {
			return fmt.Errorf("hunk[%d]: lines do not reconstruct NewText", hi)
		}
	}

	if d.OldText != oldConcat.String() {
		return fmt.Errorf("diff: hunks do not reconstruct OldText")
	}
	if d.NewText != newConcat.String() {
		return fmt.Errorf("diff: hunks do not reconstruct NewText")
	}
	return nil
}
