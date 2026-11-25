package main

import (
	"errors"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/alecthomas/kong"

	// Register conftest's custom builtins (parse_config, parse_config_file,
	// parse_combined_config_files) so conftest policies work as-is.
	_ "github.com/open-policy-agent/conftest/builtins"
)

var version = "dev"

type cli struct {
	Dir      string           `arg:"" help:"Directory (or file) of input files to evaluate."`
	Policy   string           `short:"p" help:"Rego policy file to load into the editor. Saved back to this file."`
	Lib      []string         `placeholder:"PATH" help:"Rego files or directories to load alongside the edited policy (e.g. for import data.lib...). Repeatable."`
	Data     []string         `short:"d" placeholder:"PATH" help:"JSON/YAML files or directories to load as data (same as conftest --data). Repeatable."`
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

	libs, err := loadLibs(c.Lib, c.Policy)
	if err != nil {
		return err
	}

	data, err := loadData(c.Data)
	if err != nil {
		return err
	}

	ev := &evaluator{configs: configs, combined: c.Combine, libs: libs, data: data}
	m := newModel(ev, c.Dir, c.Policy, policy, c.TabWidth)

	_, err = tea.NewProgram(m).Run()
	return err
}
