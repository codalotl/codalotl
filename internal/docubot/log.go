package docubot

import (
	"fmt"
	"github.com/codalotl/codalotl/internal/updatedocs"
	"log/slog"
	"strings"
)

// logSnippetErrors writes a consolidated, multi-line report of snippet application errors at info level. If snippetErrors is empty or logger is nil, it does nothing. The situation
// parameter should describe where the errors occurred (ex: "original application", "fix attempt").
func logSnippetErrors(logger *slog.Logger, situation string, snippetErrors []updatedocs.SnippetError) {
	if len(snippetErrors) > 0 && logger != nil {

		var sb strings.Builder
		for _, se := range snippetErrors {
			sb.WriteString(fmt.Sprintf("snippet error: %s\n", se.UserErrorMessage))
			sb.WriteString("snippet:\n")
			sb.WriteString(se.Snippet)
			sb.WriteString("\n\n")
		}
		logger.Info("snippet errors", "situation", situation, "multiline", sb.String())
	}
}

// logIdentifiersDebug logs a compact summary of the package's identifiers and highlights undocumented items. When documentTestFiles is true, test-file identifiers are included in the
// diagnostic output.
func logIdentifiersDebug(idents *Identifiers, documentTestFiles bool, options BaseOptions) {
	options.Log(idents.String())
	options.Log(idents.FilteredString(documentTestFiles))

	undocFuncs := idents.FuncIDs(false, documentTestFiles)
	options.Log("undocumented funcs", "identifiers", strings.Join(undocFuncs, ","))

	undocTypes := idents.TypeIDs(false, documentTestFiles)
	options.Log("undocumented types", "identifiers", strings.Join(undocTypes, ","))

	undocValues := idents.ValueIDs(false, documentTestFiles)
	options.Log("undocumented values", "identifiers", strings.Join(undocValues, ","))

	undocFields := idents.FieldIDs(false, documentTestFiles)
	var fieldAttrs []any // slog.Attr of (type name, "various,field,names")
	for k, v := range undocFields {
		fieldAttrs = append(fieldAttrs, slog.String(k, strings.Join(v, ",")))
	}
	options.Log("undocumented fields", fieldAttrs...)
}
