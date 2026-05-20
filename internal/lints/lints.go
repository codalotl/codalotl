package lints

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/codalotl/codalotl/internal/q/cmdrunner"
	"github.com/codalotl/codalotl/internal/specmd"
	"github.com/codalotl/codalotl/internal/updatedocs"
)

//go:embed spec_diff_instructions.md
var embeddedSpecDiffInstructions string

// SpecDiffInstructions are instructions provided to the LLM in the body of the spec diff XML response when that lint doesn't pass.
var SpecDiffInstructions = strings.TrimRight(embeddedSpecDiffInstructions, "\n")

// Situation indicates the context under which the lints are run. Internally, `SituationInitial`/`SituationTests` map to action `check`, and `SituationPatch`/`SituationFix`
// map to action `fix`.
type Situation string

// The Situation constants name the contexts in which lint steps can run.
const (
	// SituationInitial runs check-mode lints during initial context creation. Reflow is always skipped in this situation.
	SituationInitial Situation = "initial"

	SituationPatch Situation = "patch" // SituationPatch runs fix-mode lints during patch application.
	SituationFix   Situation = "fix"   // SituationFix runs fix-mode lints during an explicit fix run.
	SituationTests Situation = "tests" // SituationTests runs check-mode lints during test validation.
)

// An action is the check or fix command mode implied by a lint situation.
type action string

const (
	actionCheck action = "check"
	actionFix   action = "fix"
)

const (
	stepIDGofmt        = "gofmt"
	stepIDReflow       = "reflow"
	stepIDStaticcheck  = "staticcheck"
	stepIDGolangciLint = "golangci-lint"
	stepIDSpecDiff     = "spec-diff"
	stepIDSpecFmt      = "spec-fmt"
)

// ConfigMode represents the configuration mode of specifying steps: do we extend existing steps, or replace them all with the given steps?
type ConfigMode string

// The ConfigMode constants define how configured lint steps combine with defaults.
const (
	ConfigModeExtend  ConfigMode = "extend"  // ConfigModeExtend keeps default steps and appends configured steps.
	ConfigModeReplace ConfigMode = "replace" // ConfigModeReplace uses configured steps instead of defaults.
)

// Lints is the user-configurable lint pipeline. It is intended to live under the top-level `lints` key in config JSON.
type Lints struct {
	// Mode controls how Steps combine with the default steps. An empty mode is treated as ConfigModeExtend.
	Mode ConfigMode `json:"mode,omitempty"`

	// Disable lists step IDs to remove after defaults and configured steps are combined. Empty IDs are ignored, and steps without IDs are not disabled by this list.
	Disable []string `json:"disable,omitempty"`

	// Steps lists additional or replacement lint steps, depending on Mode. A step with only a recognized ID expands to that preconfigured step.
	Steps []Step `json:"steps,omitempty"`
}

// Reflows returns true if the lint configuration runs reflow.
func (l Lints) Reflows() bool {
	steps, err := ResolveSteps(&l, 0)
	if err != nil {
		return false
	}
	return stepsEnableReflow(steps)
}

// Step configures one lint pipeline step. A step may be a fully specified command pair or a recognized preconfigured step ID.
type Step struct {
	// Optional. Empty string means "unset". Multiple steps may have an unset ID.
	ID string `json:"id,omitempty"`

	// The step will be run in the following situations.
	//   - If omitted/null: fully specified steps run in all situations; ID-only preconfigured steps inherit their built-in situations (ex: reflow, spec-fmt, and spec-diff
	//     use limited defaults).
	//   - If []: run in no situations (disable).
	//   - Reflow is skipped in SituationInitial regardless of Situations.
	Situations []Situation `json:"situations,omitempty"`

	// Active, when set, is executed before selecting/running the step's lint command for a package. If the result is exit code 0 with no non-whitespace output: step
	// is inactive. Otherwise, active.
	Active *cmdrunner.Command `json:"active,omitempty"`

	// Check is the command used when the step runs in check mode. Steps enabled for SituationInitial or SituationTests must have a Check command.
	Check *cmdrunner.Command `json:"check,omitempty"`

	// Fix is the command preferred when the step runs in fix mode. If Fix is nil, fix mode falls back to Check so check-only lints can still run.
	Fix *cmdrunner.Command `json:"fix,omitempty"`
}

const (
	defaultReflowWidth = 120
	noIssuesFound      = "no issues found"
	noLintersStatusXML = `<lint-status ok="true" message="no linters"></lint-status>`

	templateModuleDir          = "{{ .moduleDir }}"
	templateRelativePackageDir = "{{ .relativePackageDir }}"

	inputPath               = "path"
	inputModuleDir          = "moduleDir"
	inputRelativePackageDir = "relativePackageDir"
)

const reflowCheckInstructions = "never manually fix these unless asked; fixing is automatic on apply_patch"

func normalizeReflowWidth(reflowWidth int) int {
	if reflowWidth <= 0 {
		return defaultReflowWidth
	}
	return reflowWidth
}

// DefaultSteps returns default steps. It is equivalent to ResolveSteps(nil, 0).
func DefaultSteps() []Step {
	return defaultSteps(0)
}

func defaultSteps(reflowWidth int) []Step {
	// Defaults intentionally include only lightweight, low-noise steps.
	// Additional steps (like reflow) are available as preconfigured steps by
	// specifying `{"id":"reflow"}` in config.
	gofmt, _ := preconfiguredStep(stepIDGofmt, reflowWidth)
	specFmt, _ := preconfiguredStep(stepIDSpecFmt, reflowWidth)
	specDiff, _ := preconfiguredStep(stepIDSpecDiff, reflowWidth)
	return []Step{gofmt, specFmt, specDiff}
}

// preconfiguredStep returns the built-in lint step for id. It returns ok=false for unknown IDs. A non-positive reflowWidth uses defaultReflowWidth.
func preconfiguredStep(id string, reflowWidth int) (Step, bool) {
	reflowWidth = normalizeReflowWidth(reflowWidth)

	switch id {
	case stepIDGofmt:
		gofmtCheck := newPreconfiguredCommand("gofmt",
			[]string{
				"-l",
				templateRelativePackageDir,
			},
			true,
		)
		gofmtFix := newPreconfiguredCommand("gofmt",
			[]string{
				"-l",
				"-w",
				templateRelativePackageDir,
			},
			false,
		)

		return Step{ID: stepIDGofmt, Check: gofmtCheck, Fix: gofmtFix}, true
	case stepIDReflow:
		// ID == "reflow" is special-cased during execution (it is NOT executed as a
		// subprocess). The command is still stored so users can override the args.
		reflowCheckArgs := []string{
			"docs",
			"reflow",
			"--check",
			fmt.Sprintf("--width=%d", reflowWidth),
			templateRelativePackageDir,
		}
		reflowFixArgs := []string{
			"docs",
			"reflow",
			fmt.Sprintf("--width=%d", reflowWidth),
			templateRelativePackageDir,
		}
		reflowCheck := newPreconfiguredCommand("codalotl", reflowCheckArgs, true)
		reflowFix := newPreconfiguredCommand("codalotl", reflowFixArgs, false)

		// Reflow is intentionally excluded from initial context creation.
		return Step{
			ID:         stepIDReflow,
			Situations: []Situation{SituationPatch, SituationFix, SituationTests},
			Check:      reflowCheck,
			Fix:        reflowFix,
		}, true
	case stepIDStaticcheck:
		// staticcheck has no built-in fix mode. In fix situations we still run it in
		// check mode (selectCommand falls back to Check when Fix is nil).
		staticcheckCheck := newPreconfiguredCommand("staticcheck", []string{"./" + templateRelativePackageDir}, false)

		return Step{
			ID:    stepIDStaticcheck,
			Check: staticcheckCheck,
		}, true
	case stepIDGolangciLint:
		golangciCheck := newPreconfiguredCommand("golangci-lint",
			[]string{
				"run",
				"./" + templateRelativePackageDir,
			},
			false,
		)
		golangciFix := newPreconfiguredCommand("golangci-lint",
			[]string{
				"run",
				"--fix",
				"./" + templateRelativePackageDir,
			},
			false,
		)

		return Step{
			ID:    stepIDGolangciLint,
			Check: golangciCheck,
			Fix:   golangciFix,
		}, true
	case stepIDSpecDiff:
		// ID == "spec-diff" is special-cased during execution (it is NOT executed as a
		// subprocess). The command is still stored so config validation works and so
		// we can render a uniform cmdrunner-style output.
		specDiffCheck := newPreconfiguredCommand("codalotl",
			[]string{
				"spec",
				"diff",
				templateRelativePackageDir,
			},
			true,
		)

		// Spec diffs are enabled in tests and dedicated fix flows by default (not
		// during apply_patch auto-fix).
		return Step{
			ID:         stepIDSpecDiff,
			Situations: []Situation{SituationTests, SituationFix},
			Check:      specDiffCheck,
		}, true
	case stepIDSpecFmt:
		// ID == "spec-fmt" is special-cased during execution (it is NOT executed as a
		// subprocess). The command is still stored so config validation works and so
		// we can render a uniform cmdrunner-style output.
		specFmtFix := newPreconfiguredCommand("codalotl",
			[]string{
				"spec",
				"fmt",
				templateRelativePackageDir,
			},
			false,
		)
		return Step{
			ID:         stepIDSpecFmt,
			Situations: []Situation{SituationPatch, SituationFix},
			Fix:        specFmtFix,
		}, true
	default:
		return Step{}, false
	}
}

func newPreconfiguredCommand(command string, args []string, failIfAnyOutput bool) *cmdrunner.Command {
	return &cmdrunner.Command{
		Command:                command,
		Args:                   append([]string(nil), args...),
		CWD:                    templateModuleDir,
		OutcomeFailIfAnyOutput: failIfAnyOutput,
		MessageIfNoOutput:      noIssuesFound,
	}
}

// ResolveSteps merges defaults and user config, applying disable rules. Validation errors (unknown mode, invalid step definitions, duplicate IDs, etc.) return an
// error. It normalizes width handling:
//   - `reflow` steps include `--width=<reflowWidth>` when missing.
//   - `spec-fmt` steps inherit `--width=<reflowWidth>` when reflow is enabled.
func ResolveSteps(cfg *Lints, reflowWidth int) ([]Step, error) {
	reflowWidth = normalizeReflowWidth(reflowWidth)

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

	return normalizeStepWidths(steps, reflowWidth)
}

// appendStepsUnique canonicalizes, validates, and appends src steps to dst without duplicating non-empty IDs. Existing dst IDs are treated as already used, empty
// IDs may repeat, and reflowWidth is used when expanding preconfigured ID-only steps. If an error occurs after an append, dst is not rolled back.
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

// canonicalizeStep expands an ID-only preconfigured step into its built-in definition. It preserves fully specified steps, applies non-nil Situations and Active
// overrides to the built-in step, and leaves unknown IDs unchanged for validation.
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
	if s.Active != nil {
		pre.Active = s.Active
	}
	return pre
}

// validateStep reports whether s is a valid lint step definition. Nil Situations enable all situations. The validation checks situations, required check/fix commands
// for enabled situations, command names, and attribute key/value pairs.
func validateStep(s Step) error {
	if err := validateSituations(s.Situations); err != nil {
		if s.ID == "" {
			return fmt.Errorf("lint step: %w", err)
		}
		return fmt.Errorf("lint step %q: %w", s.ID, err)
	}

	if s.Check == nil && s.Fix == nil {
		if s.ID == "" {
			return errors.New("lint step: at least one of check/fix command is required")
		}
		return fmt.Errorf("lint step %q: at least one of check/fix command is required", s.ID)
	}
	enabledInitial := stepEnabledInSituation(s, SituationInitial)
	enabledTests := stepEnabledInSituation(s, SituationTests)
	enabledPatch := stepEnabledInSituation(s, SituationPatch)
	enabledFix := stepEnabledInSituation(s, SituationFix)

	// If the step can run in a check action situation, Check is required.
	if enabledInitial || enabledTests {
		if s.Check == nil {
			if s.ID == "" {
				return errors.New("lint step: check command is required for initial/tests situations")
			}
			return fmt.Errorf("lint step %q: check command is required for initial/tests situations", s.ID)
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
	if err := validateCommand(s.ID, "active", s.Active); err != nil {
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
		case SituationInitial, SituationPatch, SituationFix, SituationTests:
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

func stepEnabledOutsideInitial(step Step) bool {
	return stepEnabledInSituation(step, SituationTests) ||
		stepEnabledInSituation(step, SituationPatch) ||
		stepEnabledInSituation(step, SituationFix)
}

// normalizeStepWidths ensures reflow-related steps carry the configured documentation width. It adds missing --width flags to reflow commands and, when reflow is
// enabled, to spec-fmt commands. Existing invalid width flags are returned as errors, and non-positive widths use defaultReflowWidth.
func normalizeStepWidths(steps []Step, reflowWidth int) ([]Step, error) {
	reflowWidth = normalizeReflowWidth(reflowWidth)

	reflowEnabled := stepsEnableReflow(steps)

	for i := range steps {
		switch steps[i].ID {
		case stepIDReflow:
			if err := normalizeStepCommandWidths(&steps[i], reflowWidth); err != nil {
				return nil, err
			}
		case stepIDSpecFmt:
			if !reflowEnabled {
				continue
			}
			if err := normalizeStepCommandWidths(&steps[i], reflowWidth); err != nil {
				return nil, err
			}
		}
	}
	return steps, nil
}

func normalizeStepCommandWidths(step *Step, reflowWidth int) error {
	commands := []struct {
		name string
		cmd  **cmdrunner.Command
	}{
		{name: "check", cmd: &step.Check},
		{name: "fix", cmd: &step.Fix},
	}
	for _, c := range commands {
		if *c.cmd == nil {
			continue
		}
		normalized, err := ensureWidthArg(*c.cmd, reflowWidth)
		if err != nil {
			return fmt.Errorf("lint step %q: %s command: %w", step.ID, c.name, err)
		}
		*c.cmd = normalized
	}
	return nil
}

func stepsEnableReflow(steps []Step) bool {
	for _, s := range steps {
		if s.ID != stepIDReflow {
			continue
		}
		// Reflow is always skipped for SituationInitial.
		if stepEnabledOutsideInitial(s) {
			return true
		}
	}
	return false
}

// ensureWidthArg returns a command whose arguments include a --width flag. It validates any existing width flag, leaves commands that already set one unchanged,
// and appends --width=<reflowWidth> to a shallow copy otherwise. Non-positive widths use defaultReflowWidth.
func ensureWidthArg(c *cmdrunner.Command, reflowWidth int) (*cmdrunner.Command, error) {
	if c == nil {
		return nil, errors.New("command is nil")
	}
	reflowWidth = normalizeReflowWidth(reflowWidth)

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

// parseWidthFlag returns the value and argument index of a --width flag in args. It accepts both --width=N and --width N forms. If no width is present, it returns
// ok=false and idx=-1. Duplicate flags, missing values, and non-integer values return an error.
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
//   - Steps that are inactive are not run, and do not contribute towards the returned XML (it's as if they weren't in steps).
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
		if s.ID == stepIDReflow && situation == SituationInitial {
			continue
		}
		selected = append(selected, s)
	}

	if len(selected) == 0 {
		return noLintersStatusXML, nil
	}

	moduleDir, relativePackageDir, err := cmdrunner.ManifestDir(sandboxDir, targetPkgAbsDir)
	if err != nil {
		return "", err
	}

	var all cmdrunner.Result

	for _, s := range selected {
		if !stepActive(ctx, sandboxDir, targetPkgAbsDir, moduleDir, relativePackageDir, s) {
			continue
		}

		if s.ID == stepIDReflow {
			c, modeAttr, dryRun, err := selectCommand(s, act)
			if err != nil {
				return "", err
			}
			cr, crErr := runReflow(moduleDir, relativePackageDir, targetPkgAbsDir, c, modeAttr, dryRun)
			if crErr != nil {
				return "", crErr
			}
			all.Results = append(all.Results, cr)
			continue
		}

		if s.ID == stepIDSpecDiff {
			// This lint is always rendered as a check-only step, even in fix situations.
			// It's executed in-process so we can use internal/specmd directly and keep
			// output uniform (cmdrunner-style).
			cr := runSpecDiff(relativePackageDir, targetPkgAbsDir)
			all.Results = append(all.Results, cr)
			continue
		}
		if s.ID == stepIDSpecFmt {
			// This lint is fix-only and is executed in-process so we can format SPEC.md
			// via internal/specmd without spawning a subprocess.
			c, _, _, err := selectCommand(s, act)
			if err != nil {
				return "", err
			}
			reflowWidth, _, hasWidth, err := parseWidthFlag(c.Args)
			if err != nil {
				return "", fmt.Errorf("lint step %q: fix command: %w", s.ID, err)
			}
			if !hasWidth {
				reflowWidth = 0
			}
			cr := runSpecFmt(moduleDir, relativePackageDir, targetPkgAbsDir, reflowWidth)
			all.Results = append(all.Results, cr)
			continue
		}

		c, modeAttr, _, err := selectCommand(s, act)
		if err != nil {
			return "", err
		}

		runner := newLintRunner()
		cmd := withModeAttr(*c, modeAttr)
		runner.AddCommand(cmd)

		r, runErr := runner.Run(ctx, sandboxDir, lintRunnerInputs(targetPkgAbsDir, moduleDir, relativePackageDir))
		if runErr != nil {
			return "", runErr
		}
		all.Results = append(all.Results, r.Results...)
	}

	if len(all.Results) == 0 {
		return noLintersStatusXML, nil
	}
	return all.ToXML("lint-status"), nil
}

// stepActive reports whether s should run for the package. The spec-diff and spec-fmt steps are active only when the package contains SPEC.md, except that unexpected
// stat errors are treated as active. If s has no Active command, it is active. Otherwise the Active command is run and the step is inactive only when that command
// exits with code 0, produces no non-whitespace output, and has no exec error.
func stepActive(ctx context.Context, sandboxDir string, targetPkgAbsDir string, moduleDir string, relativePackageDir string, s Step) bool {
	if stepRequiresSpecMD(s.ID) {
		// Special-case: this step is only active when the package has a SPEC.md.
		// (The spec describes this as a pseudo "active command"; we do it in-process
		// for portability and to avoid spawning a subprocess just to check existence.)
		_, err := os.Stat(filepath.Join(targetPkgAbsDir, "SPEC.md"))
		if err != nil {
			// Keep the pipeline quiet for packages without specs.
			// For unexpected errors (permissions, transient I/O, etc.), treat as active
			// so we surface the error in the lint output instead of silently skipping.
			return !errors.Is(err, os.ErrNotExist)
		}
	}

	if s.Active == nil {
		return true
	}
	runner := newLintRunner()
	runner.AddCommand(*s.Active)
	r, err := runner.Run(ctx, sandboxDir, lintRunnerInputs(targetPkgAbsDir, moduleDir, relativePackageDir))
	if err != nil || len(r.Results) == 0 {
		// Errors or unexpected results in the active check are considered active.
		return true
	}
	cr := r.Results[0]
	// The only way to make a step inactive is a clean 0 exit with no
	// non-whitespace output.
	return !(cr.ExitCode == 0 && strings.TrimSpace(cr.Output) == "" && cr.ExecError == nil)
}

func stepRequiresSpecMD(id string) bool {
	return id == stepIDSpecDiff || id == stepIDSpecFmt
}

func newLintRunner() *cmdrunner.Runner {
	return cmdrunner.NewRunner(
		map[string]cmdrunner.InputType{
			inputPath:               cmdrunner.InputTypePathDir,
			inputModuleDir:          cmdrunner.InputTypePathDir,
			inputRelativePackageDir: cmdrunner.InputTypeString,
		},
		[]string{inputPath, inputModuleDir, inputRelativePackageDir},
	)
}

func lintRunnerInputs(targetPkgAbsDir string, moduleDir string, relativePackageDir string) map[string]any {
	return map[string]any{
		inputPath:               targetPkgAbsDir,
		inputModuleDir:          moduleDir,
		inputRelativePackageDir: relativePackageDir,
	}
}

// selectCommand chooses the command to run for s under act. For check actions, Check is required. For fix actions, Fix is preferred and Check is used as a check-mode
// dry run when no Fix command exists. The returned modeAttr is the XML mode value, and dryRun reports whether the command should avoid modifying files.
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

// runReflow runs the in-process documentation reflow lint and returns a cmdrunner-style command result. The command's Args provide the optional --width flag. Dry
// runs fail when files would change, while fix runs fail only on reflow errors or failed identifiers. The rendered command omits the width flag.
func runReflow(moduleDir string, relativePackageDir string, targetPkgAbsDir string, c *cmdrunner.Command, modeAttr string, dryRun bool) (cmdrunner.CommandResult, error) {
	start := time.Now()

	width, _, ok, err := parseWidthFlag(c.Args)
	if err != nil {
		return cmdrunner.CommandResult{}, err
	}
	if !ok || width <= 0 {
		width = normalizeReflowWidth(width)
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
		MessageIfNoOutput: noIssuesFound,
		Attrs:             attrs,
		ExecStatus:        cmdrunner.ExecStatusCompleted,
		ExecError:         fnErr,
		Outcome:           outcome,
		Duration:          time.Since(start),
	}
	return cr, nil
}

// runSpecDiff runs the in-process spec-diff lint for targetPkgAbsDir and returns a cmdrunner-style check result. The result fails when SPEC.md cannot be read, diffs
// cannot be computed or formatted, or the implementation differs from the spec. Diff output includes SpecDiffInstructions when present.
func runSpecDiff(relativePackageDir string, targetPkgAbsDir string) cmdrunner.CommandResult {
	start := time.Now()

	specPath := filepath.Join(targetPkgAbsDir, "SPEC.md")
	s, readErr := specmd.Read(specPath)

	var out bytes.Buffer
	var diffErr error
	var diffs []specmd.SpecDiff

	if readErr == nil {
		diffs, diffErr = s.ImplementationDiffs()
	}
	if readErr == nil && diffErr == nil && len(diffs) > 0 {
		diffErr = specmd.FormatDiffs(diffs, &out)
	}

	outcome := cmdrunner.OutcomeSuccess
	var execErr error
	if readErr != nil {
		outcome = cmdrunner.OutcomeFailed
		execErr = readErr
	} else if diffErr != nil {
		outcome = cmdrunner.OutcomeFailed
		execErr = diffErr
	} else if len(diffs) > 0 {
		// FormatDiffs succeeded (no error), but there are differences.
		outcome = cmdrunner.OutcomeFailed
	}

	output := strings.TrimRight(out.String(), "\n")
	if len(diffs) > 0 && SpecDiffInstructions != "" {
		if output != "" {
			output += "\n\n"
		}
		output += SpecDiffInstructions
	}
	if execErr != nil {
		// cmdrunner's XML rendering can fall back to MessageIfNoOutput when Output is
		// empty, which is misleading for failures. Ensure errors are visible.
		specRel := relativePackageDir
		if specRel == "" {
			specRel = "SPEC.md"
		} else {
			specRel = strings.TrimSuffix(specRel, "/") + "/SPEC.md"
		}
		if output != "" {
			output += "\n\n"
		}
		output += "Error: " + specRel + ": " + execErr.Error()
	}

	cr := cmdrunner.CommandResult{
		Command:           "codalotl",
		Args:              []string{"spec", "diff", relativePackageDir},
		Output:            output,
		MessageIfNoOutput: noIssuesFound,
		Attrs:             []string{"mode", "check"},
		ExecStatus:        cmdrunner.ExecStatusCompleted,
		ExecError:         execErr,
		Outcome:           outcome,
		Duration:          time.Since(start),
	}
	return cr
}

// runSpecFmt formats the package SPEC.md in process and returns a cmdrunner-style result. relativePackageDir is used in the rendered command, moduleDir is used
// to render changed and error paths, and reflowWidth is passed to specmd formatting. The result fails on read or format errors.
func runSpecFmt(moduleDir string, relativePackageDir string, targetPkgAbsDir string, reflowWidth int) cmdrunner.CommandResult {
	start := time.Now()
	specPath := filepath.Join(targetPkgAbsDir, "SPEC.md")
	s, readErr := specmd.Read(specPath)
	modified := false
	formatErr := error(nil)
	if readErr == nil {
		modified, formatErr = s.FormatGoCodeBlocks(reflowWidth)
	}
	outcome := cmdrunner.OutcomeSuccess
	var execErr error
	if readErr != nil {
		outcome = cmdrunner.OutcomeFailed
		execErr = readErr
	} else if formatErr != nil {
		outcome = cmdrunner.OutcomeFailed
		execErr = formatErr
	}
	var output string
	if modified {
		output = relPathForOutput(moduleDir, specPath)
	}
	if execErr != nil {
		// Ensure errors render even when there are no modified files to list.
		if output != "" {
			output += "\n\n"
		}
		output += "Error: " + relPathForOutput(moduleDir, specPath) + ": " + execErr.Error()
	}
	cr := cmdrunner.CommandResult{
		Command:           "codalotl",
		Args:              []string{"spec", "fmt", relativePackageDir},
		Output:            output,
		MessageIfNoOutput: noIssuesFound,
		Attrs:             []string{"mode", "fix"},
		ExecStatus:        cmdrunner.ExecStatusCompleted,
		ExecError:         execErr,
		Outcome:           outcome,
		Duration:          time.Since(start),
	}
	return cr
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
	case SituationInitial, SituationTests:
		return actionCheck, nil
	case SituationPatch, SituationFix:
		return actionFix, nil
	default:
		return "", fmt.Errorf("unknown situation %q", string(s))
	}
}
