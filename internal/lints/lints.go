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

// Situation indicates the UX context under which the lints are run.
//
// Internally:
//   - SituationInitial / SituationCheck imply "check"
//   - SituationPatch / SituationFix imply "fix"
type Situation string

const (
	SituationInitial Situation = "initial"
	SituationPatch   Situation = "patch"
	SituationFix     Situation = "fix"
	SituationCheck   Situation = "check"
)

type action string

const (
	actionCheck action = "check"
	actionFix   action = "fix"
)

// ConfigMode represents the configuration mode of specifying steps: do we extend existing steps, or replace them all with the given steps?
type ConfigMode string

const (
	ConfigModeExtend  ConfigMode = "extend"
	ConfigModeReplace ConfigMode = "replace"
)

// Lints is the user-configurable lint pipeline. It is intended to live under the top-level `lints` key in config JSON.
type Lints struct {
	Mode    ConfigMode `json:"mode,omitempty"`
	Disable []string   `json:"disable,omitempty"`
	Steps   []Step     `json:"steps,omitempty"`
}

type Step struct {
	// Optional. Empty string means "unset". Multiple steps may have an unset ID.
	ID string `json:"id,omitempty"`

	// The step will be run in the following situations.
	//   - If omitted/nil: run in all situations.
	//   - If []: run in no situations.
	Situations []Situation `json:"situations,omitempty"`

	// Check/Fix override Cmd for their respective actions.
	Check *cmdrunner.Command `json:"check,omitempty"`

	Fix *cmdrunner.Command `json:"fix,omitempty"`
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
	// Defaults intentionally include only gofmt (reflow is available as a
	// preconfigured step by specifying `{"id":"reflow"}` in config).
	gofmt, _ := preconfiguredStep("gofmt", reflowWidth)
	return []Step{gofmt}
}

func preconfiguredStep(id string, reflowWidth int) (Step, bool) {
	if reflowWidth <= 0 {
		reflowWidth = defaultReflowWidth
	}

	switch id {
	case "gofmt":
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

		return Step{ID: "gofmt", Check: gofmtCheck, Fix: gofmtFix}, true
	case "reflow":
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

		// Reflow is intentionally excluded from initial context creation.
		return Step{
			ID:         "reflow",
			Situations: []Situation{SituationPatch, SituationFix, SituationCheck},
			Check:      reflowCheck,
			Fix:        reflowFix,
		}, true
	case "staticcheck":
		// staticcheck has no built-in fix mode. In fix situations we still run it in
		// check mode (selectCommand falls back to Check when Fix is nil).
		staticcheckCheck := &cmdrunner.Command{
			Command: "staticcheck",
			Args: []string{
				"./{{ .relativePackageDir }}",
			},
			CWD:               "{{ .moduleDir }}",
			MessageIfNoOutput: "no issues found",
		}

		return Step{
			ID:    "staticcheck",
			Check: staticcheckCheck,
		}, true
	case "golangci-lint":
		golangciCheck := &cmdrunner.Command{
			Command: "golangci-lint",
			Args: []string{
				"run",
				"./{{ .relativePackageDir }}",
			},
			CWD:               "{{ .moduleDir }}",
			MessageIfNoOutput: "no issues found",
		}
		golangciFix := &cmdrunner.Command{
			Command: "golangci-lint",
			Args: []string{
				"run",
				"--fix",
				"./{{ .relativePackageDir }}",
			},
			CWD:               "{{ .moduleDir }}",
			MessageIfNoOutput: "no issues found",
		}

		return Step{
			ID:    "golangci-lint",
			Check: golangciCheck,
			Fix:   golangciFix,
		}, true
	default:
		return Step{}, false
	}
}

// ResolveSteps merges defaults and user config, applying disable rules. Validation errors (unknown mode, invalid step definitions, duplicate IDs, etc.) return an
// error.
//
// It also normalizes any `codalotl docs reflow` step to include `--width=<reflowWidth>` when missing.
func ResolveSteps(cfg *Lints, reflowWidth int) ([]Step, error) {
	if reflowWidth <= 0 {
		reflowWidth = defaultReflowWidth
	}

	if cfg == nil {
		return defaultSteps(reflowWidth), nil
	}

	mode := cfg.Mode
	if mode == "" {
		mode = ConfigModeExtend
	}

	var steps []Step
	switch mode {
	case ConfigModeExtend:
		steps = append([]Step(nil), defaultSteps(reflowWidth)...)
		if err := appendStepsUnique(&steps, cfg.Steps, reflowWidth); err != nil {
			return nil, err
		}
	case ConfigModeReplace:
		steps = nil
		if err := appendStepsUnique(&steps, cfg.Steps, reflowWidth); err != nil {
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
			if s.ID != "" {
				if _, ok := disable[s.ID]; ok {
					continue
				}
			}
			filtered = append(filtered, s)
		}
		steps = filtered
	}

	return normalizeReflowWidth(steps, reflowWidth)
}

func appendStepsUnique(dst *[]Step, src []Step, reflowWidth int) error {
	seen := make(map[string]struct{}, len(*dst)+len(src))
	for _, s := range *dst {
		if s.ID == "" {
			continue
		}
		seen[s.ID] = struct{}{}
	}

	for _, s := range src {
		s = canonicalizeStep(s, reflowWidth)
		if err := validateStep(s); err != nil {
			return err
		}
		if s.ID != "" {
			if _, ok := seen[s.ID]; ok {
				return fmt.Errorf("duplicate lint step id %q", s.ID)
			}
			seen[s.ID] = struct{}{}
		}
		*dst = append(*dst, s)
	}
	return nil
}

func canonicalizeStep(s Step, reflowWidth int) Step {
	if s.ID == "" {
		return s
	}
	if s.Check != nil || s.Fix != nil {
		return s
	}

	// This allows config like: {"steps":[{"id":"reflow"}]} to add the preconfigured step.
	pre, ok := preconfiguredStep(s.ID, reflowWidth)
	if !ok {
		return s
	}

	// If the user explicitly provided situations (including empty slice), those win.
	if s.Situations != nil {
		pre.Situations = s.Situations
	}
	return pre
}

func validateStep(s Step) error {
	if err := validateSituations(s.Situations); err != nil {
		if s.ID == "" {
			return fmt.Errorf("lint step: %w", err)
		}
		return fmt.Errorf("lint step %q: %w", s.ID, err)
	}

	enabledInitial := stepEnabledInSituation(s, SituationInitial)
	enabledCheck := stepEnabledInSituation(s, SituationCheck)
	enabledPatch := stepEnabledInSituation(s, SituationPatch)
	enabledFix := stepEnabledInSituation(s, SituationFix)

	// If the step can run in a check action situation, Check is required.
	if enabledInitial || enabledCheck {
		if s.Check == nil {
			if s.ID == "" {
				return errors.New("lint step: check command is required for initial/check situations")
			}
			return fmt.Errorf("lint step %q: check command is required for initial/check situations", s.ID)
		}
	}

	// If the step can run in a fix action situation, it needs at least one command.
	// (Fix preferred, but Check is acceptable for check-only lints.)
	if enabledPatch || enabledFix {
		if s.Check == nil && s.Fix == nil {
			if s.ID == "" {
				return errors.New("lint step: at least one of check/fix command is required")
			}
			return fmt.Errorf("lint step %q: at least one of check/fix command is required", s.ID)
		}
	}

	if err := validateCommand(s.ID, "check", s.Check); err != nil {
		return err
	}
	if err := validateCommand(s.ID, "fix", s.Fix); err != nil {
		return err
	}
	return nil
}

func validateCommand(stepID string, which string, c *cmdrunner.Command) error {
	if c == nil {
		return nil
	}
	if c.Command == "" {
		if stepID == "" {
			return fmt.Errorf("lint step: %s command: command is required", which)
		}
		return fmt.Errorf("lint step %q: %s command: command is required", stepID, which)
	}
	if len(c.Attrs)%2 != 0 {
		if stepID == "" {
			return fmt.Errorf("lint step: %s command: attrs must have even length, got %d", which, len(c.Attrs))
		}
		return fmt.Errorf("lint step %q: %s command: attrs must have even length, got %d", stepID, which, len(c.Attrs))
	}
	return nil
}

func validateSituations(situations []Situation) error {
	if situations == nil {
		return nil
	}
	seen := make(map[Situation]struct{}, len(situations))
	for _, s := range situations {
		switch s {
		case SituationInitial, SituationPatch, SituationFix, SituationCheck:
		default:
			return fmt.Errorf("unknown situation %q", string(s))
		}
		if _, ok := seen[s]; ok {
			return fmt.Errorf("duplicate situation %q", string(s))
		}
		seen[s] = struct{}{}
	}
	return nil
}

func stepEnabledInSituation(step Step, situation Situation) bool {
	if step.Situations == nil {
		return true
	}
	for _, s := range step.Situations {
		if s == situation {
			return true
		}
	}
	return false
}

func normalizeReflowWidth(steps []Step, reflowWidth int) ([]Step, error) {
	if reflowWidth <= 0 {
		reflowWidth = defaultReflowWidth
	}

	for i := range steps {
		if steps[i].ID != "reflow" {
			continue
		}
		if steps[i].Check != nil {
			check, err := ensureWidthArg(steps[i].Check, reflowWidth)
			if err != nil {
				return nil, fmt.Errorf("lint step %q: check command: %w", steps[i].ID, err)
			}
			steps[i].Check = check
		}

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

// Run executes steps for the given situation against targetPkgAbsDir and returns cmdrunner XML (`lint-status`).
//
//   - sandboxDir is the cmdrunner rootDir.
//   - targetPkgAbsDir is an absolute package directory.
//   - Run does not stop early: it attempts to execute all steps, even if earlier steps report failures.
//   - Command failures are reflected in the XML. Hard errors (invalid config, templating failures, internal errors) return a Go error.
func Run(ctx context.Context, sandboxDir string, targetPkgAbsDir string, steps []Step, situation Situation) (string, error) {
	if sandboxDir == "" {
		return "", errors.New("sandboxDir is required")
	}
	if targetPkgAbsDir == "" {
		return "", errors.New("targetPkgAbsDir is required")
	}
	act, err := actionForSituation(situation)
	if err != nil {
		return "", err
	}

	selected := make([]Step, 0, len(steps))
	for _, s := range steps {
		if !stepEnabledInSituation(s, situation) {
			continue
		}
		// Reflow is never enabled during initial context creation, regardless of config.
		if s.ID == "reflow" && situation == SituationInitial {
			continue
		}
		selected = append(selected, s)
	}

	if len(selected) == 0 {
		return `<lint-status ok="true" message="no linters"></lint-status>`, nil
	}

	moduleDir, relativePackageDir, err := cmdrunner.ManifestDir(sandboxDir, targetPkgAbsDir)
	if err != nil {
		return "", err
	}

	var all cmdrunner.Result

	for _, s := range selected {
		c, modeAttr, dryRun, err := selectCommand(s, act)
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

func selectCommand(s Step, act action) (cmd *cmdrunner.Command, modeAttr string, dryRun bool, err error) {
	switch act {
	case actionCheck:
		if s.Check == nil {
			if s.ID == "" {
				return nil, "", false, errors.New("lint step: check command is required")
			}
			return nil, "", false, fmt.Errorf("lint step %q: check command is required", s.ID)
		}
		return s.Check, "check", true, nil
	case actionFix:
		if s.Fix != nil {
			return s.Fix, "fix", false, nil
		}
		if s.Check != nil {
			return s.Check, "check", true, nil
		}
		if s.ID == "" {
			return nil, "", false, errors.New("lint step: at least one of check/fix command is required")
		}
		return nil, "", false, fmt.Errorf("lint step %q: at least one of check/fix command is required", s.ID)
	default:
		return nil, "", false, fmt.Errorf("unknown action %q", string(act))
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
	// Intentionally do NOT render `--width=` in output; it can distract the LLM,
	// and width is fully automated.
	args = append(args, relativePackageDir)

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

func actionForSituation(s Situation) (action, error) {
	switch s {
	case SituationInitial, SituationCheck:
		return actionCheck, nil
	case SituationPatch, SituationFix:
		return actionFix, nil
	default:
		return "", fmt.Errorf("unknown situation %q", string(s))
	}
}
