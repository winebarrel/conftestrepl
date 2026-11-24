# conftestrepl

A TUI REPL for writing [Rego](https://www.openpolicyagent.org/docs/latest/policy-language/) policies against [conftest](https://www.conftest.dev/) inputs.

Edit a policy in the left pane. The right pane shows the evaluated value of `data.<package>` for each input file, and it updates as you type. You see the whole package value, not just deny/warn pass or fail.

```
╭──────────────────────────────────╮╭──────────────────────────────────╮
│Policy (Rego)               p.rego││Output                     3 files│
│package main                      ││{                                 │
│                                  ││  "inputdir/deployment.yaml": {   │
│deny contains msg if {            ││    "deny": [                     │
│    input.kind == "Deployment"    ││      "containers must not run…"  │
│    msg := "containers must not…" ││    ],                            │
│}                                 ││    "is_deployment": true         │
│                                  ││  },                              │
│is_deployment if input.kind == …  ││  ...                             │
╰──────────────────────────────────╯╰──────────────────────────────────╯
inputdir/ · 3 files   mode: per-file          ^S save  ^O switch  ^C quit
```

## Installation

```sh
go install github.com/winebarrel/conftestrepl@latest
```

## Usage

```
Usage: conftestrepl <dir> [flags]

Arguments:
  <dir>    Directory (or file) of input files to evaluate.

Flags:
  -h, --help             Show context-sensitive help.
  -p, --policy=STRING    Rego policy file to load into the editor. Saved back to
                         this file.
      --combine          Combine all inputs into one input and evaluate once
                         (same as conftest --combine).
      --tab-width=4      Number of spaces a tab is converted to.
      --version          Show version.
```

### Inputs

Inputs are handled the same way as in conftest. conftestrepl walks `<dir>` recursively, follows symlinks, and parses every file conftest can parse (YAML, JSON, HCL, Dockerfile, TOML, and others).

`data.conftest.file.name` and `data.conftest.file.dir` are set during evaluation, so existing conftest policies still work. All OPA builtins are available, plus conftest's own `parse_config`, `parse_config_file` and `parse_combined_config_files`.

### Output

Per-file mode (the default) evaluates each file and returns an object keyed by file path. A multi-document YAML file is evaluated one document at a time, and its results are returned as an array.

```json
{
  "inputdir/a.yaml": {"deny": ["..."], "warn": []},
  "inputdir/k8s.yaml": [
    {"deny": ["..."]},
    {"deny": []}
  ]
}
```

With `--combine`, all files are combined into one `input` of the form `[{"path": ..., "contents": ...}, ...]`, the same as `conftest --combine`. The policy is evaluated once, and the result is a single object.

```json
{"deny": ["..."]}
```

The policy is evaluated again 300ms after you stop typing. A new evaluation cancels the one still running, and each evaluation times out after 10 seconds. If the policy has an error, the error is shown above the last good result, which stays on screen dimmed.

### Keys

Editing uses the standard [Bubble Tea textarea](https://github.com/charmbracelet/bubbles) keys, including arrows, Home/End, `C-f` `C-b` `C-n` `C-p` `C-a` `C-e` `M-f` `M-b` `C-d` `C-k` `C-u` `C-w` and `M-d`.

| Key | Action |
| --- | --- |
| `C-y` | Insert the text last deleted with `C-k` / `C-u` / `C-w` / `M-d` |
| `Tab` | Insert `--tab-width` spaces |
| `Ctrl+S` | Save the policy (to `-p`, or prompts for a file name; `Esc` or `Ctrl+C` cancels the prompt) |
| `Ctrl+O` | Switch focus between the editor and the output (the output scrolls with arrows / PgUp / PgDn) |
| `Ctrl+C` | Quit (press twice if there are unsaved changes) |

The editor holds no tab characters. Tabs in a loaded file or in pasted text become `--tab-width` spaces. Mouse input is not captured, so you can select and copy text with your terminal.

## Limitations

- Only the single module in the editor is evaluated. Other policy or library modules (such as `import data.lib...`) and `--data` files are not loaded.
- The policy must use Rego v1 syntax. Rego v0 syntax is not supported.
