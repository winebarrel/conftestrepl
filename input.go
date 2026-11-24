package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/open-policy-agent/conftest/parser"
)

// collectFiles walks dir the same way conftest does (runner/test.go getFilesFromDirectory):
// it recurses into directories, follows symlinks, and keeps only files conftest can parse.
func collectFiles(dir string) ([]string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		if !parser.FileSupported(dir) {
			return nil, fmt.Errorf("unsupported file: %s", dir)
		}
		return []string{dir}, nil
	}

	var files []string
	visited := map[string]bool{}

	if err := filepath.Walk(dir, walkFn(visited, &files)); err != nil {
		return nil, err
	}

	return files, nil
}

func walkFn(visited map[string]bool, files *[]string) filepath.WalkFunc {
	return func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("walk path: %w", err)
		}

		if info.IsDir() {
			if visited[path] {
				return filepath.SkipDir
			}
			visited[path] = true
			return nil
		}

		if info.Mode()&os.ModeSymlink == 0 {
			if parser.FileSupported(path) {
				*files = append(*files, path)
			}
			return nil
		}

		realPath, err := filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}

		ri, err := os.Stat(realPath)
		if err != nil {
			return err
		}

		if ri.IsDir() {
			return filepath.Walk(realPath, walkFn(visited, files))
		}

		if parser.FileSupported(realPath) {
			*files = append(*files, realPath)
		}

		return nil
	}
}

// loadInputs collects and parses the files under dir.
// The result maps a file path to its parsed contents; multi-document files have a []any value.
func loadInputs(dir string) (map[string]any, error) {
	files, err := collectFiles(dir)
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no supported files found in %s", dir)
	}

	configs, err := parser.ParseConfigurations(files)
	if err != nil {
		return nil, fmt.Errorf("parse configurations: %w", err)
	}

	return configs, nil
}
