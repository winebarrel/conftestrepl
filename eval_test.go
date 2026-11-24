package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const testPolicy = `package main

deny contains msg if {
	input.kind == "Deployment"
	msg := "no root"
}

warn contains msg if {
	input.spec.type == "NodePort"
	msg := "NodePort is discouraged"
}

is_deployment if input.kind == "Deployment"

file := data.conftest.file.name
`

func mustLoad(t *testing.T) map[string]any {
	t.Helper()
	configs, err := loadInputs(filepath.Join("testdata", "inputs"))
	if err != nil {
		t.Fatal(err)
	}
	return configs
}

func evalJSON(t *testing.T, ev *evaluator, src string) any {
	t.Helper()
	out, err := ev.evaluate(context.Background(), "policy.rego", src)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out)
	}
	return v
}

func TestCollectFiles(t *testing.T) {
	files, err := collectFiles(filepath.Join("testdata", "inputs"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"testdata/inputs/config.json",
		"testdata/inputs/deployment.yaml",
		"testdata/inputs/multi.yaml",
		"testdata/inputs/sub/service.yaml",
	}
	if !reflect.DeepEqual(files, want) {
		t.Errorf("got %v, want %v", files, want)
	}
}

func TestEvaluatePerFile(t *testing.T) {
	ev := &evaluator{configs: mustLoad(t)}
	got := evalJSON(t, ev, strings.ReplaceAll(testPolicy, "\t", "  ")).(map[string]any)

	dep := got["testdata/inputs/deployment.yaml"].(map[string]any)
	if !reflect.DeepEqual(dep["deny"], []any{"no root"}) {
		t.Errorf("deny = %v", dep["deny"])
	}
	if dep["is_deployment"] != true {
		t.Errorf("is_deployment = %v", dep["is_deployment"])
	}
	if dep["file"] != "deployment.yaml" {
		t.Errorf("data.conftest.file.name = %v", dep["file"])
	}

	svc := got["testdata/inputs/sub/service.yaml"].(map[string]any)
	if !reflect.DeepEqual(svc["warn"], []any{"NodePort is discouraged"}) {
		t.Errorf("warn = %v", svc["warn"])
	}
	if _, ok := svc["is_deployment"]; ok {
		t.Errorf("undefined rule should be absent: %v", svc)
	}

	multi, ok := got["testdata/inputs/multi.yaml"].([]any)
	if !ok || len(multi) != 2 {
		t.Fatalf("multi-document result = %v", got["testdata/inputs/multi.yaml"])
	}
	if multi[1].(map[string]any)["file"] != "multi.yaml" {
		t.Errorf("multi[1] = %v", multi[1])
	}
}

func TestEvaluateCombined(t *testing.T) {
	ev := &evaluator{configs: mustLoad(t), combined: true}
	src := `package main

configmaps := {c.contents.metadata.name |
  some c in input
  c.contents.kind == "ConfigMap"
}

deny contains msg if {
  some c in input
  c.contents.kind == "Deployment"
  some v in c.contents.spec.template.spec.volumes
  not configmaps[v.configMap.name]
  msg := sprintf("%s: configmap %s not found", [c.path, v.configMap.name])
}

count_inputs := count(input)
file := data.conftest.file.name
`
	got := evalJSON(t, ev, src).(map[string]any)

	want := map[string]any{
		"configmaps":   []any{"app-config"},
		"deny":         []any{"testdata/inputs/deployment.yaml: configmap extra-config not found"},
		"count_inputs": float64(5),
		"file":         "Combined",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestEvaluateEmptyAndError(t *testing.T) {
	ev := &evaluator{configs: mustLoad(t)}

	out, err := ev.evaluate(context.Background(), "policy.rego", "  \n")
	if err != nil || out != "" {
		t.Errorf("empty policy: out=%q err=%v", out, err)
	}

	_, err = ev.evaluate(context.Background(), "policy.rego", "package main\n\ndeny contains msg if {\n  input.spec.")
	if err == nil || !strings.Contains(err.Error(), "rego_parse_error") {
		t.Errorf("expected parse error, got %v", err)
	}

	_, err = ev.evaluate(context.Background(), "policy.rego", "package main\n\nx := undefined_fn(1)\n")
	if err == nil || !strings.Contains(err.Error(), "rego_type_error") {
		t.Errorf("expected compile error, got %v", err)
	}
}
