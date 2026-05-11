package cli

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"

	"github.com/codalotl/codalotl/internal/docubot"
	"github.com/codalotl/codalotl/internal/gocas/casclarify"
	"github.com/codalotl/codalotl/internal/gocode"
	qcli "github.com/codalotl/codalotl/internal/q/cli"
	"github.com/codalotl/codalotl/internal/q/health"
	"github.com/codalotl/codalotl/internal/q/remotemonitor"
)

var findInPlayClarifyRecords = casclarify.FindInPlay
var runDocubotImproveFromClarifications = docubot.ImproveFromClarifications

type docsImproveFromClarifySummary struct {
	ProcessedRecords     int
	DeletedRecords       int
	DocumentationChanges int
}

func newDocsImproveFromClarifyCommand(runWithConfig runWithConfigFunc) *qcli.Command {
	cmd := &qcli.Command{
		Name:             "improve-from-clarify",
		Short:            "Improve documentation from clarify_public_api records.",
		Long:             "Improves package documentation from in-play clarify_public_api CAS records for the current module and deletes each successfully processed record.",
		NoPositionalArgs: true,
		Args:             qcli.NoArgs,
		Example: strings.TrimSpace(`
codalotl docs improve-from-clarify
`),
		Run: runWithConfig("docs_improve_from_clarify", func(c *qcli.Context, cfg Config, _ *remotemonitor.Monitor) error {
			summary, err := runDocsImproveFromClarify(c, cfg)
			if err != nil {
				return err
			}
			return writeDocsImproveFromClarifySummary(c.Out, summary)
		}),
	}
	return cmd
}

func runDocsImproveFromClarify(c *qcli.Context, cfg Config) (docsImproveFromClarifySummary, error) {
	wd, err := os.Getwd()
	if err != nil {
		return docsImproveFromClarifySummary{}, err
	}
	mod, err := gocode.NewModule(wd)
	if err != nil {
		return docsImproveFromClarifySummary{}, err
	}
	db, err := casDBForBaseDir(mod.AbsolutePath)
	if err != nil {
		return docsImproveFromClarifySummary{}, err
	}
	records, err := findInPlayClarifyRecords(db, mod)
	if err != nil {
		return docsImproveFromClarifySummary{}, err
	}
	sort.Slice(records, func(i, j int) bool {
		ti := strings.TrimSpace(records[i].TargetPackage)
		tj := strings.TrimSpace(records[j].TargetPackage)
		if ti != tj {
			return ti < tj
		}
		return records[i].Path < records[j].Path
	})

	summary := docsImproveFromClarifySummary{}
	for _, record := range records {
		groups, err := clarificationGroupsForRecord(record)
		if err != nil {
			return summary, err
		}

		targets := make([]string, 0, len(groups))
		for target := range groups {
			targets = append(targets, target)
		}
		sort.Strings(targets)

		recordChanges := 0
		for _, target := range targets {
			pkg, _, err := loadPackageArg(target)
			if err != nil {
				return summary, err
			}
			changes, err := runDocubotImproveFromClarifications(pkg, groups[target], docubot.ImproveFromClarificationsOptions{
				BaseOptions: docubot.BaseOptions{
					ReflowMaxWidth: cfg.ReflowWidth,
					Context:        c.Context,
					Out:            c.Out,
					Model:          effectiveModel(cfg),
					Ctx:            health.NewCtx(slog.New(slog.NewTextHandler(io.Discard, nil))),
				},
			})
			if err != nil {
				return summary, err
			}
			recordChanges += len(changes)
		}

		if err := record.Delete(); err != nil {
			return summary, err
		}
		summary.ProcessedRecords++
		summary.DeletedRecords++
		summary.DocumentationChanges += recordChanges
	}
	return summary, nil
}

func clarificationGroupsForRecord(record casclarify.InPlayRecord) (map[string][]docubot.Clarification, error) {
	groups := map[string][]docubot.Clarification{}
	defaultTarget := strings.TrimSpace(record.TargetPackage)

	for _, entry := range record.Metadata.Entries {
		target := strings.TrimSpace(entry.TargetPackage)
		if target == "" {
			target = defaultTarget
		}
		if target == "" {
			return nil, fmt.Errorf("clarify record %s has an entry without a target package", record.Path)
		}
		groups[target] = append(groups[target], docubot.Clarification{
			Identifier: entry.Identifier,
			Question:   entry.Question,
			Answer:     entry.Answer,
		})
	}

	return groups, nil
}

func writeDocsImproveFromClarifySummary(w io.Writer, summary docsImproveFromClarifySummary) error {
	_, err := fmt.Fprintf(
		w,
		"Processed %d clarify record(s); deleted %d; applied %d documentation change(s).\n",
		summary.ProcessedRecords,
		summary.DeletedRecords,
		summary.DocumentationChanges,
	)
	return err
}
