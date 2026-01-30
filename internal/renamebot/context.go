package renamebot

import (
	"github.com/codalotl/codalotl/internal/gocode"
	"strings"
)

func contextForFile(pkg *gocode.Package, file *gocode.File, ps *packageSummary) string {
	var b strings.Builder

	b.WriteString("// File: ")
	b.WriteString(file.FileName)
	b.WriteString("\n\n")

	b.Write(file.Contents)

	b.WriteString("\n-----\n\n")

	b.WriteString("## Naming conventions of select identifiers across the entire package (not just this file):\n")

	psFile := ps.relevantForFile(file.FileName)

	b.WriteString(psFile.String())
	b.WriteString("\n")

	return b.String()
}
