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
	result, err := filterEnvironment([]byte(strings.Join(encoded, "\x00")), root, nil, "")
	if err != nil {
		panic(err)
	}
	return result
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
	var after []string
	for _, key := range keys {
		after = append(after, key+"=/project/config")
	}
	result := capture(mapFromEntries(after), "/project")
	if len(result) != len(keys) {
		t.Fatalf("explicit exports lost: %#v", result)
	}
	for _, key := range keys {
		if result[key] != "/workspace/config" {
			t.Errorf("%s not preserved and translated: %q", key, result[key])
		}
	}
}

func mapFromEntries(entries []string) map[string]string {
	result := make(map[string]string, len(entries))
	for _, entry := range entries {
		key, value, _ := strings.Cut(entry, "=")
		result[key] = value
	}
	return result
}

func TestHostInterpolationBeforePathFiltering(t *testing.T) {
	host := map[string]string{
		"PATH":  "/project/bin:/nix/store/host-tool/bin:/host/other",
		"TOKEN": "#{OTHER}",
		"EMPTY": "",
	}
	input := []byte("PATH=/nix/store/inner/bin:#{PATH}\x00CONFIG=at-#{TOKEN}-#{EMPTY}\x00")
	result, err := filterEnvironment(input, "/project", host, "")
	if err != nil {
		t.Fatal(err)
	}
	if result["PATH"] != "/nix/store/inner/bin:/workspace/bin:/nix/store/host-tool/bin:/bin:/usr/bin" {
		t.Fatalf("PATH: %q", result["PATH"])
	}
	if result["CONFIG"] != "at-#{OTHER}-" {
		t.Fatalf("interpolation should make one pass: %q", result["CONFIG"])
	}
}

func TestMissingHostReferenceFails(t *testing.T) {
	_, err := filterEnvironment([]byte("CONFIG=#{MISSING}\x00"), "/project", nil, "")
	if err == nil || !strings.Contains(err.Error(), "MISSING") {
		t.Fatalf("expected a missing-variable error, got %v", err)
	}
}

func TestHostPathSpecAppendsToNixPath(t *testing.T) {
	input := []byte("PATH=/nix/store/inner/bin\x00")
	host := map[string]string{"PATH": "/nix/store/host/bin:/unmounted/bin"}
	result, err := filterEnvironment(input, "/project", host, "#{PATH}")
	if err != nil {
		t.Fatal(err)
	}
	if result["PATH"] != "/nix/store/inner/bin:/nix/store/host/bin:/bin:/usr/bin" {
		t.Fatalf("PATH: %q", result["PATH"])
	}
}
