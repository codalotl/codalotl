package cli

import (
	"fmt"
	"io"
	"strings"
)

func writeAlignedTable(w io.Writer, headers []string, rows [][]string) error {
	cols := make([][]string, len(headers))
	for i, h := range headers {
		cols[i] = []string{h}
	}
	for _, row := range rows {
		for i, v := range row {
			cols[i] = append(cols[i], v)
		}
	}

	widths := make([]int, len(cols))
	for i := range cols {
		for _, v := range cols[i] {
			if n := len(v); n > widths[i] {
				widths[i] = n
			}
		}
	}

	writeRow := func(values []string) error {
		for i, v := range values {
			if i > 0 {
				if _, err := io.WriteString(w, "  "); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintf(w, "%-*s", widths[i], v); err != nil {
				return err
			}
		}
		_, err := io.WriteString(w, "\n")
		return err
	}

	if err := writeRow(headers); err != nil {
		return err
	}

	separators := make([]string, len(headers))
	for i := range headers {
		separators[i] = strings.Repeat("-", widths[i])
	}
	if err := writeRow(separators); err != nil {
		return err
	}

	for _, row := range rows {
		if err := writeRow(row); err != nil {
			return err
		}
	}
	return nil
}
