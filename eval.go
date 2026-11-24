package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/open-policy-agent/conftest/parser"
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/storage/inmem"
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

// evaluate compiles src and evaluates data.<package> for each input (or once for the
// combined input). An empty policy evaluates to an empty string with no error.
func (e *evaluator) evaluate(ctx context.Context, filename string, src string) (string, error) {
	if strings.TrimSpace(src) == "" {
		return "", nil
	}

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
		return evalOne(ctx, query, store, path, input)
	}

	var out any

	if e.combined {
		out, err = run(combinedPath, parser.CombineConfigurations(e.configs)[combinedPath])
		if err != nil {
			return "", err
		}
	} else {
		results := map[string]any{}

		for path, config := range e.configs {
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

func evalOne(ctx context.Context, query rego.PreparedEvalQuery, store storage.Store, path string, input any) (any, error) {
	if err := writeFileInfo(ctx, store, path); err != nil {
		return nil, err
	}

	value, err := ast.InterfaceToValue(input)
	if err != nil {
		return nil, fmt.Errorf("convert input: %w", err)
	}

	rs, err := query.Eval(ctx, rego.EvalParsedInput(value))
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
