package main

import (
	"strings"
	"testing"
)

func capture(entries map[string]string, root string) map[string]string {
	var encoded []string
	for key, value := range entries {
		encoded = append(encoded, key+"="+value)
	}
	return filterEnvironment(nil, []byte(strings.Join(encoded, "\x00")), root)
}

func TestCapturedProjectPathsAreTranslatedBeforeFiltering(t *testing.T) {
	result := capture(map[string]string{
		"PROJECT_BIN": "/host/project/bin",
		"PATH":        "/host/project/bin:/host/unmounted:/nix/store/package/bin:/host/project/bin:/workspace/../outside:relative",
	}, "/host/project")
	if result["PROJECT_BIN"] != "/workspace/bin" {
		t.Fatalf("project path: %q", result["PROJECT_BIN"])
	}
	want := "/workspace/bin:/nix/store/package/bin:/bin:/usr/bin"
	if result["PATH"] != want {
		t.Fatalf("PATH: %q want %q", result["PATH"], want)
	}
}

func TestLiteralReferencesAreNotExpanded(t *testing.T) {
	literal := "$PROJECT_BIN:${HOME}:$$HOME:$(touch /must-not-run)"
	result := capture(map[string]string{
		"HOME":        "/host/private",
		"PROJECT_BIN": "/host/project/bin",
		"LITERAL":     literal,
		"PATH":        "$PROJECT_BIN:/host/project/bin",
	}, "/host/project")
	if result["LITERAL"] != literal {
		t.Fatalf("literal value changed: %q", result["LITERAL"])
	}
	if result["PATH"] != "/workspace/bin:/bin:/usr/bin" {
		t.Fatalf("literal PATH entry was retained: %q", result["PATH"])
	}
	if result["HOME"] != "/host/private" {
		t.Fatal("explicit HOME export must be retained")
	}
}

func TestProjectPathBoundariesAndTraversal(t *testing.T) {
	result := capture(map[string]string{
		"BOUNDARIES": "/project-other:/other/project/bin:/project/bin:/project",
		"PATH":       "/project/../../outside:/nix/store/../../outside:/project/bin:/project/bin",
	}, "/project")
	if result["BOUNDARIES"] != "/project-other:/other/project/bin:/workspace/bin:/workspace" {
		t.Fatalf("path boundaries: %q", result["BOUNDARIES"])
	}
	if result["PATH"] != "/workspace/bin:/bin:/usr/bin" {
		t.Fatalf("path traversal survived: %q", result["PATH"])
	}
}

func TestEnvironmentDeltaDoesNotExportInheritedValues(t *testing.T) {
	before := []byte("UNCHANGED=inherited\x00MODIFIED=old\x00PATH=/host/bin\x00EMPTY=\x00REMOVED=gone\x00")
	after := []byte("UNCHANGED=inherited\x00MODIFIED=new\x00PATH=/project/bin:/host/bin\x00EMPTY=\x00ADDED=\x00HOME=/host/new\x00CDS_PRIVATE=private\x00")
	result := filterEnvironment(before, after, "/project")
	if len(result) != 4 || result["MODIFIED"] != "new" || result["ADDED"] != "" || result["PATH"] != "/workspace/bin:/bin:/usr/bin" || result["HOME"] != "/host/new" {
		t.Fatalf("unexpected delta: %#v", result)
	}
	for _, key := range []string{"UNCHANGED", "EMPTY", "REMOVED", "CDS_PRIVATE"} {
		if _, exists := result[key]; exists {
			t.Fatalf("excluded or unchanged value %s exported", key)
		}
	}
}

func TestBuildURLAndLiteralDollarPathSurvive(t *testing.T) {
	result := capture(map[string]string{
		"URL":  "https://example.test/build/latest?value=$UNKNOWN",
		"PATH": "/project/cash$literal:/workspace/bin$literal:/host/unmounted$literal:/nix/store/pkg/bin",
	}, "/project")
	if result["URL"] != "https://example.test/build/latest?value=$UNKNOWN" {
		t.Fatalf("URL discarded or changed: %q", result["URL"])
	}
	expected := "/workspace/cash$literal:/workspace/bin$literal:/nix/store/pkg/bin:/bin:/usr/bin"
	if result["PATH"] != expected {
		t.Fatalf("literal dollar PATH: %q", result["PATH"])
	}
}

func TestExplicitRuntimeExportsArePreserved(t *testing.T) {
	keys := []string{"HOME", "USER", "LOGNAME", "SHELL", "TMP", "TMPDIR", "TEMP", "TEMPDIR", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_RUNTIME_DIR", "NIX_PATH", "NIX_BUILD_TOP", "PWD", "OLDPWD"}
	var before, after []string
	for _, key := range keys {
		before = append(before, key+"=unchanged-host")
		after = append(after, key+"=/project/config")
	}
	result := filterEnvironment([]byte(strings.Join(before, "\x00")), []byte(strings.Join(after, "\x00")), "/project")
	if len(result) != len(keys) {
		t.Fatalf("explicit exports lost: %#v", result)
	}
	for _, key := range keys {
		if result[key] != "/workspace/config" {
			t.Errorf("%s not preserved and translated: %q", key, result[key])
		}
	}
}

func TestCaptureInternalsCannotLeakEvenWhenModified(t *testing.T) {
	keys := []string{"_", "SHLVL", "direnv", "DIRENV_DIR", "DIRENV_WATCHES", "DIRENV_DIFF", "NIX_DIRENV_DID_FALLBACK", "CDS_CAPTURE_FILTER", "CDS_CAPTURE_PROJECT", "BASH_FUNC_helper%%"}
	var before, after []string
	for _, key := range keys {
		before = append(before, key+"=before")
		after = append(after, key+"=after")
	}
	result := filterEnvironment([]byte(strings.Join(before, "\x00")), []byte(strings.Join(after, "\x00")), "/project")
	if len(result) != 0 {
		t.Fatalf("capture internals leaked: %#v", result)
	}
}
