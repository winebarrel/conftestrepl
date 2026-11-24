package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/alecthomas/kong"
	tea "github.com/charmbracelet/bubbletea"

	// Register conftest's custom builtins (parse_config, parse_config_file,
	// parse_combined_config_files) so conftest policies work as-is.
	_ "github.com/open-policy-agent/conftest/builtins"
)

var version = "dev"

type cli struct {
	Dir      string           `arg:"" help:"Directory (or file) of input files to evaluate."`
	Policy   string           `short:"p" help:"Rego policy file to load into the editor. Saved back to this file."`
	Combine  bool             `help:"Combine all inputs into one input and evaluate once (same as conftest --combine)."`
	TabWidth int              `default:"4" help:"Number of spaces a tab is converted to."`
	Version  kong.VersionFlag `help:"Show version."`
}

func main() {
	var c cli
	kong.Parse(&c, kong.Vars{"version": version}, kong.Description("Interactive Rego REPL for conftest inputs."))

	if err := run(&c); err != nil {
		fmt.Fprintln(os.Stderr, "conftestrepl:", err)
		os.Exit(1)
	}
}

func run(c *cli) error {
	if c.TabWidth < 1 {
		return errors.New("--tab-width must be >= 1")
	}

	configs, err := loadInputs(c.Dir)
	if err != nil {
		return err
	}

	var policy string
	if c.Policy != "" {
		b, err := os.ReadFile(c.Policy)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		policy = expandTabs(string(b), c.TabWidth)
	}

	ev := &evaluator{configs: configs, combined: c.Combine}
	m := newModel(ev, c.Dir, c.Policy, policy, c.TabWidth)

	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}
