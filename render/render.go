// Package render implements the `qm render` command.
//
// Spec: spec-render.yaml.
//
//	qm render                     -> run <project>/render.ps1
//	qm render a,b                 -> quarto render --profile a,b --no-clean
//	qm render slides,a            -> run <project>/.scripts/make-slides.ps1 a
//	qm render a                   -> error (exactly two profiles required
//	                                 when profile names are given)
//
// PowerShell scripts (*.ps1) are executed via `pwsh` when the current
// shell is not itself a PowerShell session. `quarto` is invoked
// directly because it is a normal cross-platform executable.
package render

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/christophberger/start"
)

var projectPath *string

// Register wires the `render` command into start. `render` has no
// sub-commands, so — as with `lint` — it is registered as a leaf.
func Register(projectFlag *string) {
	projectPath = projectFlag
	start.Add(&start.Command{
		Name:  "render",
		Short: "Render one or more Quarto profiles",
		Long: "Render Quarto output. With no arguments, runs the project's " +
			"render.ps1 script. With two comma-separated profile names, " +
			"runs `quarto render --profile <a>,<b> --no-clean`, unless one " +
			"of the profiles is `slides`, in which case " +
			".scripts/make-slides.ps1 <other> is executed instead.",
		Flags: []string{"project"},
		Cmd:   cmd,
	})
}

func cmd(c *start.Command) error {
	if projectPath == nil {
		return fmt.Errorf("render: --project flag not initialised")
	}
	docPath, err := filepath.Abs(*projectPath)
	if err != nil {
		return fmt.Errorf("cannot resolve project path: %w", err)
	}
	arg := ""
	if len(c.Args) >= 1 {
		arg = c.Args[0]
	}
	return Run(docPath, arg)
}

// Plan describes what Run intends to execute. Exposed for tests.
type Plan struct {
	Name string   // logical action: "render.ps1", "make-slides", "quarto"
	Cmd  string   // executable
	Args []string // arguments
}

// BuildPlan turns the raw profile argument into an executable plan.
// docPath must be absolute. profileArg is the raw comma-joined argument
// received on the command line (may be empty).
func BuildPlan(docPath, profileArg string) (Plan, error) {
	profiles := splitProfiles(profileArg)

	switch len(profiles) {
	case 0:
		script := filepath.Join(docPath, "render.ps1")
		bin, args := psInvocation(script)
		return Plan{Name: "render.ps1", Cmd: bin, Args: args}, nil

	case 1:
		return Plan{}, fmt.Errorf(
			"render: exactly two profile names are required (got %q); " +
				"pass them as `<a>,<b>` or omit the argument to run render.ps1",
			profiles[0])

	case 2:
		if other, ok := pickSlides(profiles); ok {
			script := filepath.Join(docPath, ".scripts", "make-slides.ps1")
			bin, args := psInvocation(script, other)
			return Plan{Name: "make-slides", Cmd: bin, Args: args}, nil
		}
		return Plan{
			Name: "quarto",
			Cmd:  "quarto",
			Args: []string{"render", "--profile", profiles[0] + "," + profiles[1], "--no-clean"},
		}, nil

	default:
		return Plan{}, fmt.Errorf(
			"render: at most two profile names supported (got %d)", len(profiles))
	}
}

// Run builds and executes the plan against docPath, streaming child
// output to the current process' stdout/stderr.
func Run(docPath, profileArg string) error {
	plan, err := BuildPlan(docPath, profileArg)
	if err != nil {
		return err
	}
	c := exec.Command(plan.Cmd, plan.Args...)
	c.Dir = docPath
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("render: %s failed: %w", plan.Name, err)
	}
	return nil
}

// splitProfiles parses the raw argument into individual profile names,
// discarding empty entries so `render ""` or trailing commas don't
// produce phantom profiles.
func splitProfiles(arg string) []string {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return nil
	}
	parts := strings.Split(arg, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// pickSlides returns the non-"slides" profile if exactly one of the two
// profiles is "slides". Comparison is case-insensitive so users can
// write `Slides` or `SLIDES`.
func pickSlides(profiles []string) (string, bool) {
	a, b := profiles[0], profiles[1]
	aSlides := strings.EqualFold(a, "slides")
	bSlides := strings.EqualFold(b, "slides")
	switch {
	case aSlides && !bSlides:
		return b, true
	case bSlides && !aSlides:
		return a, true
	default:
		// Both slides or neither: no special handling.
		return "", false
	}
}

// psInvocation returns the executable and argument list needed to run a
// PowerShell script. When the current process is itself a PowerShell
// session and the OS is Windows (where .ps1 files can be executed
// directly), the script is run without wrapping. Otherwise `pwsh -File`
// is used per spec constraint PROCESS.4.
func psInvocation(script string, scriptArgs ...string) (string, []string) {
	if inPowerShell() && runtime.GOOS == "windows" {
		return script, scriptArgs
	}
	args := append([]string{"-File", script}, scriptArgs...)
	return "pwsh", args
}

// inPowerShell reports whether the current process appears to have been
// launched from a PowerShell session. Detection is heuristic — there is
// no fully reliable signal — but the combination used here catches the
// common cases (`pwsh` on macOS/Linux exports PSModulePath; PowerShell
// installers on Windows set POWERSHELL_DISTRIBUTION_CHANNEL).
func inPowerShell() bool {
	if os.Getenv("PSModulePath") == "" {
		return false
	}
	if runtime.GOOS != "windows" {
		// On non-Windows systems PSModulePath is only ever set by pwsh.
		return true
	}
	// On Windows PSModulePath is set globally, so require an additional
	// pwsh-specific signal.
	return os.Getenv("POWERSHELL_DISTRIBUTION_CHANNEL") != "" ||
		os.Getenv("PSExecutionPolicyPreference") != ""
}
