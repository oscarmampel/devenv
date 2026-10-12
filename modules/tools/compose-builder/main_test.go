package main

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvironmentPrecedenceAndLiteralValues(t *testing.T) {
	service := map[string]any{"environment": []any{"OVERRIDE=compose", "EXPLICIT_NULL"}, "container_name": "custom-container"}
	document := map[string]any{"services": map[string]any{"devshell": service, "database": map[string]any{"env_file": "./secrets/database.env"}}}
	name, err := render(document, map[string]string{"OVERRIDE": "nix", "HOME": "/workspace/custom-home", "LITERAL": "$HOME ${X}\ntrailing\\"}, "project")
	if err != nil {
		t.Fatal(err)
	}
	variables := service["environment"].(map[string]any)
	if name != "custom-container" || variables["OVERRIDE"] != "compose" || variables["HOME"] != "/workspace/custom-home" || variables["EXPLICIT_NULL"] != nil {
		t.Fatalf("precedence mismatch")
	}
	if variables["LITERAL"] != "$$HOME $${X}\ntrailing\\" {
		t.Fatal("captured dollars must remain literal after Compose interpolation")
	}
	if document["services"].(map[string]any)["database"].(map[string]any)["env_file"] != "./secrets/database.env" {
		t.Fatal("user secret paths changed")
	}
}

func TestNameAndPrivateOutput(t *testing.T) {
	service := map[string]any{}
	document := map[string]any{"services": map[string]any{"devshell": service}}
	name, err := render(document, nil, "unique-project")
	if err != nil || name != "unique-project-devshell" || service["container_name"] != name {
		t.Fatal("name is not shared with rendered document")
	}
	service["container_name"] = "${DYNAMIC}"
	if _, err := render(document, nil, "project"); err == nil {
		t.Fatal("dynamic name accepted")
	}
	path := filepath.Join(t.TempDir(), "compose.json")
	if err := writePrivate(path, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("runtime Compose must be private")
	}
}

func TestServicesSnapshotIsRequired(t *testing.T) {
	previousFlags, previousArgs := flag.CommandLine, os.Args
	defer func() { flag.CommandLine, os.Args = previousFlags, previousArgs }()
	flag.CommandLine = flag.NewFlagSet("compose-builder", flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)
	os.Args = []string{"compose-builder", "--captured", "unused-env.json", "--project", "project", "--output", "unused-compose.json"}
	if err := run(); err == nil || !strings.Contains(err.Error(), "services") {
		t.Fatalf("missing explicit services snapshot was not rejected: %v", err)
	}
}
