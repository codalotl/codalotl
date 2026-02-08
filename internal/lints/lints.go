package lints

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/codalotl/codalotl/internal/q/cmdrunner"
	"github.com/codalotl/codalotl/internal/updatedocs"
)

type Action string

const (
	ActionCheck Action = "check"
	ActionFix   Action = "fix"
)

type Mode string

const (
	ModeExtend  Mode = "extend"
	ModeReplace Mode = "replace"
)

// Lints is the user-configurable lint pipeline. It is intended to live under the
// top-level `lints` key in config JSON.
type Lints struct {
	Mode    Mode     `json:"mode,omitempty"`
	Disable []string `json:"disable,omitempty"`
	Steps   []Step   `json:"steps,omitempty"`
}

type Step struct {
	ID string `json:"id,omitempty"`

	// Check/Fix override Cmd for their respective actions.
	Check *cmdrunner.Command `json:"check,omitempty"`
	Fix   *cmdrunner.Command `json:"fix,omitempty"`
}

const defaultReflowWidth = 120

const reflowCheckInstructions = "never manually fix these unless asked; fixing is automatic on apply_patch"

// DefaultSteps returns the default lint steps.
//
// It is equivalent to ResolveSteps(nil, 0).
func DefaultSteps() []Step {
	return defaultSteps(0)
}

func defaultSteps(reflowWidth int) []Step {
	if reflowWidth <= 0 {
		reflowWidth = defaultReflowWidth
	}

	gofmtCheck := &cmdrunner.Command{
		Command: "gofmt",
		Args: []string{
			"-l",
			"{{ .relativePackageDir }}",
		},
		CWD:                    "{{ .moduleDir }}",
		OutcomeFailIfAnyOutput: true,
		MessageIfNoOutput:      "no issues found",
	}
	gofmtFix := &cmdrunner.Command{
		Command: "gofmt",
		Args: []string{
			"-l",
			"-w",
			"{{ .relativePackageDir }}",
		},
		CWD:                    "{{ .moduleDir }}",
		OutcomeFailIfAnyOutput: false,
		MessageIfNoOutput:      "no issues found",
	}

	// ID == "reflow" is special-cased during execution (it is NOT executed as a
	// subprocess). The command is still stored so users can override the args.
	reflowCheckArgs := []string{
		"docs",
		"reflow",
		"--check",
		fmt.Sprintf("--width=%d", reflowWidth),
		"{{ .relativePackageDir }}",
	}
	reflowFixArgs := []string{
		"docs",
		"reflow",
		fmt.Sprintf("--width=%d", reflowWidth),
		"{{ .relativePackageDir }}",
	}
	reflowCheck := &cmdrunner.Command{
		Command:                "codalotl",
		Args:                   append([]string(nil), reflowCheckArgs...),
		CWD:                    "{{ .moduleDir }}",
		OutcomeFailIfAnyOutput: true,
		MessageIfNoOutput:      "no issues found",
	}
	reflowFix := &cmdrunner.Command{
		Command:                "codalotl",
		Args:                   append([]string(nil), reflowFixArgs...),
		CWD:                    "{{ .moduleDir }}",
		OutcomeFailIfAnyOutput: false,
		MessageIfNoOutput:      "no issues found",
	}

	return []Step{
		{ID: "gofmt", Check: gofmtCheck, Fix: gofmtFix},
		{ID: "reflow", Check: reflowCheck, Fix: reflowFix},
	}
}

// ResolveSteps merges defaults and user config, applying disable rules.
// Validation errors (unknown mode, invalid step definitions, duplicate IDs, etc.)
// return an error.
//
// It also normalizes any `codalotl docs reflow` step to include `--width=<reflowWidth>`
// when missing.
func ResolveSteps(cfg *Lints, reflowWidth int) ([]Step, error) {
	if reflowWidth <= 0 {
		reflowWidth = defaultReflowWidth
	}

	if cfg == nil {
		return defaultSteps(reflowWidth), nil
	}

	mode := cfg.Mode
	if mode == "" {
		mode = ModeExtend
	}

	var steps []Step
	switch mode {
	case ModeExtend:
		steps = append([]Step(nil), defaultSteps(reflowWidth)...)
		if err := appendStepsUnique(&steps, cfg.Steps); err != nil {
			return nil, err
		}
	case ModeReplace:
		steps = nil
		if err := appendStepsUnique(&steps, cfg.Steps); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown lints mode %q", string(mode))
	}

	if len(cfg.Disable) > 0 {
		disable := make(map[string]struct{}, len(cfg.Disable))
		for _, id := range cfg.Disable {
			if id == "" {
				continue
			}
			disable[id] = struct{}{}
		}

		filtered := steps[:0]
		for _, s := range steps {
			if _, ok := disable[s.ID]; ok {
				continue
			}
			filtered = append(filtered, s)
		}
		steps = filtered
	}

	return normalizeReflowWidth(steps, reflowWidth)
}

func appendStepsUnique(dst *[]Step, src []Step) error {
	seen := make(map[string]struct{}, len(*dst)+len(src))
	for _, s := range *dst {
		if s.ID == "" {
			continue
		}
		seen[s.ID] = struct{}{}
	}

	for _, s := range src {
		if err := validateStep(s); err != nil {
			return err
		}
		if _, ok := seen[s.ID]; ok {
			return fmt.Errorf("duplicate lint step id %q", s.ID)
		}
		seen[s.ID] = struct{}{}
		*dst = append(*dst, s)
	}
	return nil
}

func validateStep(s Step) error {
	if s.ID == "" {
		return errors.New("lint step id is required")
	}
	if s.Check == nil {
		return fmt.Errorf("lint step %q: check command is required", s.ID)
	}
	if err := validateCommand(s.ID, "check", s.Check); err != nil {
		return err
	}
	if s.Fix != nil {
		if err := validateCommand(s.ID, "fix", s.Fix); err != nil {
			return err
		}
	}
	return nil
}

func validateCommand(stepID string, which string, c *cmdrunner.Command) error {
	if c == nil {
		return fmt.Errorf("lint step %q: %s command is nil", stepID, which)
	}
	if c.Command == "" {
		return fmt.Errorf("lint step %q: %s command: command is required", stepID, which)
	}
	if len(c.Attrs)%2 != 0 {
		return fmt.Errorf("lint step %q: %s command: attrs must have even length, got %d", stepID, which, len(c.Attrs))
	}
	return nil
}

func normalizeReflowWidth(steps []Step, reflowWidth int) ([]Step, error) {
	if reflowWidth <= 0 {
		reflowWidth = defaultReflowWidth
	}

	for i := range steps {
		if steps[i].ID != "reflow" {
			continue
		}
		check, err := ensureWidthArg(steps[i].Check, reflowWidth)
		if err != nil {
			return nil, fmt.Errorf("lint step %q: check command: %w", steps[i].ID, err)
		}
		steps[i].Check = check

		if steps[i].Fix != nil {
			fix, err := ensureWidthArg(steps[i].Fix, reflowWidth)
			if err != nil {
				return nil, fmt.Errorf("lint step %q: fix command: %w", steps[i].ID, err)
			}
			steps[i].Fix = fix
		}
	}
	return steps, nil
}

func ensureWidthArg(c *cmdrunner.Command, reflowWidth int) (*cmdrunner.Command, error) {
	if c == nil {
		return nil, errors.New("command is nil")
	}
	if reflowWidth <= 0 {
		reflowWidth = defaultReflowWidth
	}

	_, _, ok, err := parseWidthFlag(c.Args)
	if err != nil {
		return nil, err
	}
	if ok {
		return c, nil
	}

	cc := *c
	cc.Args = append([]string(nil), c.Args...)
	cc.Args = append(cc.Args, fmt.Sprintf("--width=%d", reflowWidth))
	return &cc, nil
}

func parseWidthFlag(args []string) (width int, idx int, ok bool, err error) {
	width = 0
	idx = -1
	ok = false

	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "--width=") {
			if ok {
				return 0, -1, false, errors.New("multiple --width flags")
			}
			val := strings.TrimPrefix(a, "--width=")
			n, convErr := strconv.Atoi(val)
			if convErr != nil {
				return 0, -1, false, fmt.Errorf("invalid --width value %q", val)
			}
			width, idx, ok = n, i, true
			continue
		}
		if a == "--width" {
			if ok {
				return 0, -1, false, errors.New("multiple --width flags")
			}
			if i+1 >= len(args) {
				return 0, -1, false, errors.New("--width requires a value")
			}
			n, convErr := strconv.Atoi(args[i+1])
			if convErr != nil {
				return 0, -1, false, fmt.Errorf("invalid --width value %q", args[i+1])
			}
			width, idx, ok = n, i, true
			i++ // skip the value
			continue
		}
	}
	return width, idx, ok, nil
}

// Run executes steps for the given action against targetPkgAbsDir and returns cmdrunner XML (`lint-status`).
//
// - sandboxDir is the cmdrunner rootDir.
// - targetPkgAbsDir is an absolute package directory.
// - Run does not stop early: it attempts to execute all steps, even if earlier steps report failures.
// - Command failures are reflected in the XML. Hard errors (invalid config, templating failures, internal errors) return a Go error.
func Run(ctx context.Context, sandboxDir string, targetPkgAbsDir string, steps []Step, action Action) (string, error) {
	if sandboxDir == "" {
		return "", errors.New("sandboxDir is required")
	}
	if targetPkgAbsDir == "" {
		return "", errors.New("targetPkgAbsDir is required")
	}
	switch action {
	case ActionCheck, ActionFix:
	default:
		return "", fmt.Errorf("unknown action %q", string(action))
	}

	if len(steps) == 0 {
		return `<lint-status ok="true" message="no linters"></lint-status>`, nil
	}

	moduleDir, relativePackageDir, err := cmdrunner.ManifestDir(sandboxDir, targetPkgAbsDir)
	if err != nil {
		return "", err
	}

	var all cmdrunner.Result

	for _, s := range steps {
		if s.ID == "" {
			return "", errors.New("lint step id is required")
		}
		if s.Check == nil {
			return "", fmt.Errorf("lint step %q: check command is required", s.ID)
		}

		c, modeAttr, dryRun, err := selectCommand(s, action)
		if err != nil {
			return "", err
		}

		if s.ID == "reflow" {
			cr, crErr := runReflow(moduleDir, relativePackageDir, targetPkgAbsDir, c, modeAttr, dryRun)
			if crErr != nil {
				return "", crErr
			}
			all.Results = append(all.Results, cr)
			continue
		}

		runner := cmdrunner.NewRunner(
			map[string]cmdrunner.InputType{
				"path":               cmdrunner.InputTypePathDir,
				"moduleDir":          cmdrunner.InputTypePathDir,
				"relativePackageDir": cmdrunner.InputTypeString,
			},
			[]string{"path", "moduleDir", "relativePackageDir"},
		)
		cmd := withModeAttr(*c, modeAttr)
		runner.AddCommand(cmd)

		r, runErr := runner.Run(ctx, sandboxDir, map[string]any{
			"path":               targetPkgAbsDir,
			"moduleDir":          moduleDir,
			"relativePackageDir": relativePackageDir,
		})
		if runErr != nil {
			return "", runErr
		}
		all.Results = append(all.Results, r.Results...)
	}

	return all.ToXML("lint-status"), nil
}

func selectCommand(s Step, action Action) (cmd *cmdrunner.Command, modeAttr string, dryRun bool, err error) {
	switch action {
	case ActionCheck:
		return s.Check, "check", true, nil
	case ActionFix:
		if s.Fix != nil {
			return s.Fix, "fix", false, nil
		}
		return s.Check, "check", true, nil
	default:
		return nil, "", false, fmt.Errorf("unknown action %q", string(action))
	}
}

func withModeAttr(c cmdrunner.Command, mode string) cmdrunner.Command {
	c.Args = append([]string(nil), c.Args...)
	c.Attrs = upsertAttrPair(c.Attrs, "mode", mode)
	return c
}

func upsertAttrPair(attrs []string, key string, value string) []string {
	out := make([]string, 0, len(attrs)+2)
	for i := 0; i+1 < len(attrs); i += 2 {
		k := attrs[i]
		v := attrs[i+1]
		if k == key {
			continue
		}
		out = append(out, k, v)
	}
	out = append(out, key, value)
	return out
}

func runReflow(moduleDir string, relativePackageDir string, targetPkgAbsDir string, c *cmdrunner.Command, modeAttr string, dryRun bool) (cmdrunner.CommandResult, error) {
	start := time.Now()

	width, _, ok, err := parseWidthFlag(c.Args)
	if err != nil {
		return cmdrunner.CommandResult{}, err
	}
	if !ok || width <= 0 {
		width = defaultReflowWidth
	}

	modified, failed, fnErr := updatedocs.ReflowDocumentationPaths(
		[]string{targetPkgAbsDir},
		dryRun,
		updatedocs.Options{ReflowMaxWidth: width},
	)

	sort.Strings(modified)
	sort.Strings(failed)

	var outLines []string
	for _, f := range modified {
		outLines = append(outLines, relPathForOutput(moduleDir, f))
	}
	if len(failed) > 0 {
		outLines = append(outLines, fmt.Sprintf("Failed identifiers (%d):", len(failed)))
		for _, id := range failed {
			outLines = append(outLines, "- "+id)
		}
	}
	if fnErr != nil {
		outLines = append(outLines, "Error: "+fnErr.Error())
	}

	outcome := cmdrunner.OutcomeSuccess
	if fnErr != nil || len(failed) > 0 || (dryRun && len(modified) > 0) {
		outcome = cmdrunner.OutcomeFailed
	}

	args := []string{"docs", "reflow"}
	if dryRun {
		args = append(args, "--check")
	}
	args = append(args, fmt.Sprintf("--width=%d", width), relativePackageDir)

	attrs := []string{"mode", modeAttr}
	if modeAttr == "check" {
		attrs = append(attrs, "instructions", reflowCheckInstructions)
	}

	cr := cmdrunner.CommandResult{
		Command:           "codalotl",
		Args:              args,
		Output:            strings.Join(outLines, "\n"),
		MessageIfNoOutput: "no issues found",
		Attrs:             attrs,
		ExecStatus:        cmdrunner.ExecStatusCompleted,
		ExecError:         fnErr,
		Outcome:           outcome,
		Duration:          time.Since(start),
	}
	return cr, nil
}

func relPathForOutput(sandboxDir string, p string) string {
	if sandboxDir == "" || p == "" {
		return p
	}
	r, err := filepath.Rel(sandboxDir, p)
	if err != nil {
		return p
	}
	return filepath.ToSlash(r)
}
