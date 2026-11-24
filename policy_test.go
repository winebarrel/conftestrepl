package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadLibsExcludesEditedPolicy(t *testing.T) {
	libs, err := loadLibs([]string{filepath.Join("testdata", "policy")}, filepath.Join("testdata", "policy", "main.rego"))
	if err != nil {
		t.Fatal(err)
	}

	var files []string
	for _, m := range libs {
		files = append(files, filepath.ToSlash(m.Package.Location.File))
	}
	want := []string{"testdata/policy/lib/k8s.rego", "testdata/policy/main_extra.rego"}
	if !reflect.DeepEqual(files, want) {
		t.Errorf("got %v, want %v", files, want)
	}
}

func TestLoadLibsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.rego"), []byte("package lib\n\nx := \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadLibs([]string{dir}, ""); err == nil || !strings.Contains(err.Error(), "bad.rego") {
		t.Errorf("expected a parse error naming bad.rego, got %v", err)
	}
}

func TestLoadData(t *testing.T) {
	data, err := loadData([]string{filepath.Join("testdata", "data")})
	if err != nil {
		t.Fatal(err)
	}
	// Like conftest (OPA's loader), files in subdirectories are nested under the directory name.
	want := map[string]any{
		"allowed_names": []any{"other"},
		"sub":           map[string]any{"limits": map[string]any{"replicas": float64(3)}},
	}
	if !reflect.DeepEqual(normalize(t, data), want) {
		t.Errorf("got %v, want %v", data, want)
	}
}

func TestEvaluateWithLibsAndData(t *testing.T) {
	policyPath := filepath.Join("testdata", "policy", "main.rego")
	src, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatal(err)
	}

	libs, err := loadLibs([]string{filepath.Join("testdata", "policy")}, policyPath)
	if err != nil {
		t.Fatal(err)
	}
	data, err := loadData([]string{filepath.Join("testdata", "data")})
	if err != nil {
		t.Fatal(err)
	}

	ev := &evaluator{configs: mustLoad(t), libs: libs, data: data}

	src2 := string(src) + "\nreplicas := data.sub.limits.replicas\nfile := data.conftest.file.name\n"
	out, err := ev.evaluate(context.Background(), policyPath, src2)
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)

	dep := got["testdata/inputs/deployment.yaml"].(map[string]any)
	want := map[string]any{
		"deny":     []any{"app is not allowed"},
		"extra":    true,
		"replicas": float64(3),
		"file":     "deployment.yaml",
	}
	if !reflect.DeepEqual(dep, want) {
		t.Errorf("deployment.yaml = %v, want %v", dep, want)
	}

	// The shared data must not be modified by evaluation.
	if _, ok := data["conftest"]; ok {
		t.Errorf("evaluate modified the shared data: %v", data)
	}

	// Without the lib, the import resolves to nothing and deny is empty.
	ev.libs = nil
	out, err = ev.evaluate(context.Background(), policyPath, string(src))
	if err != nil {
		t.Fatal(err)
	}
	if d := decode(t, out)["testdata/inputs/deployment.yaml"].(map[string]any)["deny"]; !reflect.DeepEqual(d, []any{}) {
		t.Errorf("deny without lib = %v", d)
	}
}

func TestEvaluateLibErrorIsReported(t *testing.T) {
	libs, err := loadLibs([]string{filepath.Join("testdata", "policy", "lib")}, "")
	if err != nil {
		t.Fatal(err)
	}
	ev := &evaluator{configs: map[string]any{"x.json": map[string]any{}}, libs: libs}

	// Rules from the lib are type-checked together with the edited policy.
	_, err = ev.evaluate(context.Background(), "policy.rego", "package main\n\nimport data.lib.k8s\n\nx := k8s.name(1)\n")
	if err == nil || !strings.Contains(err.Error(), "rego_type_error") {
		t.Errorf("expected a type error, got %v", err)
	}
}

func TestStoreDataKeepsConftestKey(t *testing.T) {
	ev := &evaluator{data: map[string]any{"conftest": map[string]any{"custom": 1}}}
	got := ev.storeData()["conftest"].(map[string]any)
	if got["custom"] != 1 || got["file"] == nil {
		t.Errorf("storeData conftest = %v", got)
	}
	if _, ok := ev.data["conftest"].(map[string]any)["file"]; ok {
		t.Error("storeData modified the shared data")
	}
}

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, s)
	}
	return v
}

// normalize round-trips v through JSON so it can be compared with plain Go values.
func normalize(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
