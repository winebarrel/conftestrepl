# conftestrepl

[![CI](https://github.com/winebarrel/conftestrepl/actions/workflows/ci.yml/badge.svg)](https://github.com/winebarrel/conftestrepl/actions/workflows/ci.yml)

A TUI REPL for writing [Rego](https://www.openpolicyagent.org/docs/latest/policy-language/) policies against [conftest](https://www.conftest.dev/) inputs.

Edit a policy in the left pane. The right pane shows the evaluated value of `data.<package>` for each input file, and it updates as you type. You see the whole package value, not just deny/warn pass or fail.

![](https://github.com/user-attachments/assets/29d5a60d-9e9e-4a8a-88f6-ea9af024c7a3)

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
      --lib=PATH         Rego files or directories to load alongside the edited
                         policy (e.g. for import data.lib...). Repeatable.
  -d, --data=PATH        JSON/YAML files or directories to load as data (same as
                         conftest --data). Repeatable.
      --combine          Combine all inputs into one input and evaluate once
                         (same as conftest --combine).
      --tab-width=4      Number of spaces a tab is converted to.
      --version          Show version.
```

### Inputs

Inputs are handled the same way as in conftest. conftestrepl walks `<dir>` recursively, follows symlinks, and parses every file conftest can parse (YAML, JSON, HCL, Dockerfile, TOML, and others).

`data.conftest.file.name` and `data.conftest.file.dir` are set during evaluation, so existing conftest policies still work. All OPA builtins are available, plus conftest's own `parse_config`, `parse_config_file` and `parse_combined_config_files`.

### Libraries and data

`--lib` loads Rego modules that you don't edit, such as the `lib` packages your policy imports. A directory is searched recursively for `.rego` files, the same way conftest loads its `-p` directories. The file opened with `-p` is skipped, so you can point `--lib` at the whole policy directory:

```sh
conftestrepl -p policy/deployment.rego --lib policy/ inputdir/
```

Modules in the same package as the edited policy (for example, other files in `package main`) are merged into its output.

`-d` / `--data` loads JSON and YAML documents into `data`, the same as `conftest --data`. Files in subdirectories are nested under the directory name. For example, `-d data/` loads `data/sub/x.yaml` as `data.sub`.

Libraries and data are loaded once, at startup. Restart conftestrepl to pick up changes to them.

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
| `Ctrl+R` | Search the policy. The cursor jumps to the first match as you type; `Ctrl+R` again goes to the next match (wrapping around), `Enter` stays there, and `Esc` or `Ctrl+C` returns to where the search started. Case-insensitive unless the query has an upper-case letter |
| `Ctrl+O` | Switch focus between the editor and the output (the output scrolls with arrows / PgUp / PgDn) |
| `Ctrl+C` | Quit (press twice if there are unsaved changes) |

The editor holds no tab characters. Tabs in a loaded file or in pasted text become `--tab-width` spaces. Mouse input is not captured, so you can select and copy text with your terminal.

## Limitations

- Only the module in the editor can be edited. Changes to `--lib` and `--data` files are not reloaded while conftestrepl is running.
- The policy must use Rego v1 syntax. Rego v0 syntax is not supported.
