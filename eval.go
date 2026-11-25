package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/open-policy-agent/conftest/parser"
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/storage/inmem"
	"github.com/open-policy-agent/opa/v1/topdown/print"
)

// combinedPath is the file name conftest uses for data.conftest.file in combined mode.
const combinedPath = "Combined"

// evaluator evaluates a Rego policy against parsed inputs and returns data.<package> as JSON.
type evaluator struct {
	configs  map[string]any
	combined bool
	// libs are extra modules (--lib) compiled together with the edited policy.
	libs []*ast.Module
	// data is the document loaded with --data.
	data map[string]any
}

// evaluate is evaluateWithPrints without the print output.
func (e *evaluator) evaluate(ctx context.Context, filename string, src string) (string, error) {
	out, _, err := e.evaluateWithPrints(ctx, filename, src)
	return out, err
}

// evaluateWithPrints compiles src and evaluates data.<package> for each input (or once for
// the combined input). It also returns the output of print() calls, one line per call,
// prefixed with the location of the call (and, per file, the input path).
// An empty policy evaluates to an empty string with no error.
func (e *evaluator) evaluateWithPrints(ctx context.Context, filename string, src string) (string, []string, error) {
	if strings.TrimSpace(src) == "" {
		return "", nil, nil
	}

	prints := &printCollector{}
	out, err := e.eval(ctx, filename, src, prints)
	return out, prints.lines, err
}

func (e *evaluator) eval(ctx context.Context, filename string, src string, prints *printCollector) (string, error) {

	module, err := ast.ParseModuleWithOpts(filename, src, ast.ParserOptions{RegoVersion: ast.RegoV1})
	if err != nil {
		return "", err
	}

	// Each evaluation gets its own store: data.conftest.file is rewritten per input,
	// and an evaluation that was canceled may still be running.
	store := inmem.NewFromObject(e.storeData())

	opts := []func(*rego.Rego){
		rego.Query(module.Package.Path.String()),
		rego.ParsedModule(module),
		rego.Store(store),
		rego.EnablePrintStatements(true),
	}
	for _, lib := range e.libs {
		// The compiler rewrites modules, so give it a copy of the shared ones.
		opts = append(opts, rego.ParsedModule(lib.Copy()))
	}

	query, err := rego.New(opts...).PrepareForEval(ctx)
	if err != nil {
		return "", err
	}

	run := func(path string, input any) (any, error) {
		prefix := ""
		if !e.combined {
			prefix = "[" + path + "] "
		}
		hook := &printHook{prefix: prefix, out: prints}
		return evalOne(ctx, query, store, path, input, hook)
	}

	var out any

	if e.combined {
		out, err = run(combinedPath, parser.CombineConfigurations(e.configs)[combinedPath])
		if err != nil {
			return "", err
		}
	} else {
		results := map[string]any{}

		// Evaluate in path order so print output comes out in a stable order.
		paths := make([]string, 0, len(e.configs))
		for path := range e.configs {
			paths = append(paths, path)
		}
		slices.Sort(paths)

		for _, path := range paths {
			config := e.configs[path]
			// Multi-document files are evaluated per document, like conftest does.
			if docs, ok := config.([]any); ok {
				docResults := make([]any, 0, len(docs))

				for _, doc := range docs {
					r, err := run(path, doc)
					if err != nil {
						return "", fmt.Errorf("%s: %w", path, err)
					}
					docResults = append(docResults, r)
				}

				results[path] = docResults
				continue
			}

			r, err := run(path, config)
			if err != nil {
				return "", fmt.Errorf("%s: %w", path, err)
			}
			results[path] = r
		}

		out = results
	}

	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}

	return string(b), nil
}

// storeData returns the --data documents plus an empty data.conftest.file.
// The maps from e.data are not modified (the store copies what is written to it).
func (e *evaluator) storeData() map[string]any {
	root := make(map[string]any, len(e.data)+1)
	for k, v := range e.data {
		root[k] = v
	}

	conftest := map[string]any{}
	if m, ok := root["conftest"].(map[string]any); ok {
		for k, v := range m {
			conftest[k] = v
		}
	}
	conftest["file"] = map[string]any{}
	root["conftest"] = conftest

	return root
}

func evalOne(ctx context.Context, query rego.PreparedEvalQuery, store storage.Store, path string, input any, hook print.Hook) (any, error) {
	if err := writeFileInfo(ctx, store, path); err != nil {
		return nil, err
	}

	value, err := ast.InterfaceToValue(input)
	if err != nil {
		return nil, fmt.Errorf("convert input: %w", err)
	}

	rs, err := query.Eval(ctx, rego.EvalParsedInput(value), rego.EvalPrintHook(hook))
	if err != nil {
		return nil, err
	}

	if len(rs) == 0 || len(rs[0].Expressions) == 0 {
		return map[string]any{}, nil
	}

	return rs[0].Expressions[0].Value, nil
}

// writeFileInfo sets data.conftest.file the same way conftest's addFileInfo does,
// so existing conftest policies keep working.
func writeFileInfo(ctx context.Context, store storage.Store, path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("get absolute path: %w", err)
	}

	return storage.WriteOne(ctx, store, storage.ReplaceOp, storage.Path{"conftest", "file"}, map[string]any{
		"name": filepath.Base(abs),
		"dir":  filepath.Dir(abs),
	})
}

// printCollector gathers print() output across the inputs of one evaluation.
type printCollector struct {
	mu    sync.Mutex
	lines []string
}

// printHook formats print() calls like conftest does ("file:row: msg").
type printHook struct {
	prefix string
	out    *printCollector
}

func (h *printHook) Print(pctx print.Context, msg string) error {
	line := h.prefix
	if loc := pctx.Location; loc != nil {
		line += fmt.Sprintf("%s:%d: ", loc.File, loc.Row)
	}
	line += msg

	h.out.mu.Lock()
	defer h.out.mu.Unlock()
	h.out.lines = append(h.out.lines, line)
	return nil
}
