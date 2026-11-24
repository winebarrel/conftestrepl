package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/bundle"
	"github.com/open-policy-agent/opa/v1/loader"
)

// loadLibs loads every .rego file under paths, like conftest loads its -p directories.
// The file being edited (exclude) is skipped so its rules aren't defined twice.
func loadLibs(paths []string, exclude string) ([]*ast.Module, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	var excludeAbs string
	if exclude != "" {
		abs, err := filepath.Abs(exclude)
		if err != nil {
			return nil, err
		}
		excludeAbs = abs
	}

	result, err := loader.NewFileLoader().
		WithProcessAnnotation(true).
		WithRegoVersion(ast.RegoV1).
		Filtered(paths, func(path string, info os.FileInfo, _ int) bool {
			if info.IsDir() {
				return false
			}
			if !strings.HasSuffix(info.Name(), bundle.RegoExt) {
				return true
			}
			abs, err := filepath.Abs(path)
			return err == nil && abs == excludeAbs
		})
	if err != nil {
		return nil, fmt.Errorf("load libs: %w", err)
	}

	parsed := result.ParsedModules()
	names := make([]string, 0, len(parsed))
	for name := range parsed {
		names = append(names, name)
	}
	slices.Sort(names)

	modules := make([]*ast.Module, 0, len(names))
	for _, name := range names {
		modules = append(modules, parsed[name])
	}

	return modules, nil
}

// loadData loads JSON and YAML documents under paths, like conftest --data.
func loadData(paths []string) (map[string]any, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	result, err := loader.NewFileLoader().Filtered(paths, func(_ string, info os.FileInfo, _ int) bool {
		if info.IsDir() {
			return false
		}
		return !slices.Contains([]string{".yaml", ".yml", ".json"}, filepath.Ext(info.Name()))
	})
	if err != nil {
		return nil, fmt.Errorf("load data: %w", err)
	}

	return result.Documents, nil
}
