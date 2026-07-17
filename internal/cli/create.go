package cli

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/dccoding1118/gogents/internal/provision"
)

// ErrWorkspaceNotEmpty is returned when the target workspace holds files and the
// user neither confirmed the write nor passed --force.
var ErrWorkspaceNotEmpty = errors.New("workspace is not empty")

// CreateOpts is `gogents runtime create` after flag parsing (D08 §3.5).
type CreateOpts struct {
	Name        string
	Template    string
	Workspace   string
	RepoURL     string
	Branch      string
	Agent       string
	Model       string
	Source      string
	Label       string
	Instance    string
	TicketsPath string
	Force       bool
}

// Validate checks required flags, enums and per-source compatibility, and
// applies the defaults (Workspace←./<name>, Label←Name, Branch←main).
func (o *CreateOpts) Validate() error {
	if o.Name == "" {
		return errors.New("name is required")
	}
	var missing []string
	if o.RepoURL == "" {
		missing = append(missing, "--repo")
	}
	if o.Agent == "" {
		missing = append(missing, "--agent")
	}
	if o.Model == "" {
		missing = append(missing, "--model")
	}
	if o.Source == "" {
		missing = append(missing, "--source")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required flags: %s", strings.Join(missing, ", "))
	}

	switch o.Agent {
	case "claude":
	default:
		return fmt.Errorf("unsupported --agent %q (supported: claude)", o.Agent)
	}

	switch o.Source {
	case "github_issue":
		if o.Instance == "" {
			return errors.New("--instance is required for --source github_issue")
		}
		if o.TicketsPath != "" {
			return errors.New("--tickets-dir applies to --source local only")
		}
	case "local":
		if o.Instance != "" {
			return errors.New("--instance applies to --source github_issue only")
		}
	default:
		return fmt.Errorf("unsupported --source %q (supported: local, github_issue)", o.Source)
	}

	if o.Workspace == "" {
		o.Workspace = "." + string(filepath.Separator) + o.Name
	}
	if o.Label == "" {
		o.Label = o.Name
	}
	if o.Branch == "" {
		o.Branch = "main"
	}
	return nil
}

// Create provisions the workspace described by opts (D08 B8). confirm is called
// only when the target is a non-empty directory and --force was not passed;
// returning false aborts without writing anything. written lists what landed and
// is returned on failure too — provision never deletes what it wrote.
func Create(opts CreateOpts, confirm func(entries []provision.Entry) bool) (written []string, err error) {
	if vErr := opts.Validate(); vErr != nil {
		return nil, vErr
	}

	// Resolve before inspecting the target: an unresolvable template must fail
	// before anything is written and before we ask the user to confirm an
	// overwrite that was never going to happen.
	src, tErr := provision.Resolve(opts.Template)
	if tErr != nil {
		return nil, tErr
	}

	state, sErr := provision.Inspect(opts.Workspace)
	if sErr != nil {
		return nil, sErr
	}

	switch state {
	case provision.TargetNotDir:
		return nil, fmt.Errorf("%w: %s", provision.ErrWorkspaceExists, opts.Workspace)
	case provision.TargetNonEmptyDir:
		if !opts.Force {
			entries, pErr := provision.Plan(src, opts.Workspace)
			if pErr != nil {
				return nil, pErr
			}
			if !confirm(entries) {
				return nil, fmt.Errorf("%w: %s", ErrWorkspaceNotEmpty, opts.Workspace)
			}
		}
	case provision.TargetAbsent, provision.TargetEmptyDir:
	}

	return provision.Materialize(src, opts.Workspace, provision.Spec{
		Name:        opts.Name,
		RepoURL:     opts.RepoURL,
		Branch:      opts.Branch,
		Agent:       opts.Agent,
		Model:       opts.Model,
		Source:      opts.Source,
		Label:       opts.Label,
		Instance:    opts.Instance,
		TicketsPath: opts.TicketsPath,
	})
}

// runCreate handles `gogents runtime create <name> [flags]`. The name is pulled
// off before flag parsing: the flag package stops at the first non-flag argument.
func runCreate(args []string) int {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		printCreateUsage()
		return 1
	}

	var o CreateOpts
	o.Name = args[0]

	fs := flag.NewFlagSet("runtime create", flag.ContinueOnError)
	fs.StringVar(&o.Template, "template", "", "template tree: path or name (default: the built-in template)")
	fs.StringVar(&o.Workspace, "workspace", "", "workspace root directory (default ./<name>)")
	fs.StringVar(&o.RepoURL, "repo", "", "repository URL (required)")
	fs.StringVar(&o.Branch, "branch", "", "default branch (default main)")
	fs.StringVar(&o.Agent, "agent", "", "agent type: claude (required)")
	fs.StringVar(&o.Model, "model", "", "agent model (required)")
	fs.StringVar(&o.Source, "source", "", "task source: local|github_issue (required)")
	fs.StringVar(&o.Label, "label", "", "task filter label (default <name>)")
	fs.StringVar(&o.Instance, "instance", "", "owner/repo (required for --source github_issue)")
	fs.StringVar(&o.TicketsPath, "tickets-dir", "", "ticket inbox dir (--source local only)")
	fs.BoolVar(&o.Force, "force", false, "skip the confirmation prompt on a non-empty workspace")
	if err := fs.Parse(args[1:]); err != nil {
		return 1
	}

	// An empty --template means "built-in" to provision, which is what omitting
	// the flag already says. `--template=` is a mistake, not a way to ask for the
	// default, and only this layer can tell the two apart.
	if templateGivenEmpty(fs) {
		fmt.Fprintln(os.Stderr, "error: --template needs a path or a name; omit it to use the built-in template")
		printCreateUsage()
		return 1
	}

	// Validate here as well as inside Create: it applies the defaults, and this
	// layer reports the resolved workspace path back to the user.
	if err := o.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		printCreateUsage()
		return 1
	}

	confirm := interactiveConfirm
	if !stdinIsTerminal() {
		confirm = refuseNonInteractive
	}

	written, err := Create(o, confirm)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		if len(written) > 0 {
			fmt.Fprintf(os.Stderr, "these files were already written and are left in place:\n")
			for _, w := range written {
				fmt.Fprintf(os.Stderr, "  %s\n", w)
			}
		}
		return 1
	}

	fmt.Printf("created workspace %s\n", o.Workspace)
	for _, w := range written {
		fmt.Printf("  %s\n", w)
	}
	fmt.Printf("\nnext: gogents start --workspace %s\n", o.Workspace)
	return 0
}

// templateGivenEmpty reports whether --template was passed with an empty value.
// Visit only walks flags that were actually set, which is what separates
// `--template=` from an omitted flag — both leave the string empty.
func templateGivenEmpty(fs *flag.FlagSet) bool {
	given := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "template" && f.Value.String() == "" {
			given = true
		}
	})
	return given
}

// interactiveConfirm lists what would land and asks before writing into a
// non-empty workspace.
func interactiveConfirm(entries []provision.Entry) bool {
	fmt.Println("workspace is not empty. The agent runtime configuration below will be written;")
	fmt.Println("files marked (overwrite) already exist. Everything else is left untouched.")
	for _, e := range entries {
		if e.Overwrite {
			fmt.Printf("  %s (overwrite)\n", e.Path)
			continue
		}
		fmt.Printf("  %s\n", e.Path)
	}
	fmt.Print("continue? [y/N]: ")

	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "y" || ans == "yes"
}

// refuseNonInteractive stands in for the prompt when stdin is not a terminal:
// silently overwriting a user's directory from a script is worse than failing.
func refuseNonInteractive(_ []provision.Entry) bool {
	fmt.Fprintln(os.Stderr, "workspace is not empty and stdin is not a terminal; pass --force to write anyway")
	return false
}

// stdinIsTerminal reports whether we can actually prompt. A ModeCharDevice check
// is not enough: /dev/null is a character device too, so `< /dev/null` would be
// mistaken for a terminal.
func stdinIsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

func printCreateUsage() {
	fmt.Fprintln(os.Stderr, "usage: gogents runtime create <name> --repo <url> --agent <type> --model <model> --source <local|github_issue>")
	fmt.Fprintln(os.Stderr, "                               [--template <path|name>] [--workspace <path>] [--branch <branch>]")
	fmt.Fprintln(os.Stderr, "                               [--label <label>] [--instance <owner/repo>] [--tickets-dir <path>] [--force]")
}
