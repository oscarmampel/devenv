package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

type call struct {
	Operation string
	Arguments []string
	Document  string
}

// The compiled test binary also acts as a controlled host tool, with no interpreter.
func TestMain(m *testing.M) {
	if os.Getenv("CDS_HELPER_TOOL") == "1" {
		operation := ""
		for _, arg := range os.Args[1:] {
			if arg == "up" || arg == "exec" || arg == "down" {
				operation = arg
				break
			}
		}
		document := ""
		if operation == "up" || operation == "down" {
			data, _ := io.ReadAll(os.Stdin)
			document = string(data)
		}
		file, _ := os.OpenFile(os.Getenv("CDS_CALLS"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		_ = json.NewEncoder(file).Encode(call{operation, os.Args[1:], document})
		file.Close()
		if operation == "exec" && os.Getenv("CDS_HOLD_EXEC") == "1" {
			for {
				time.Sleep(time.Second)
			}
		}
		status, _ := strconv.Atoi(os.Getenv("CDS_STATUS_" + strings.ToUpper(operation)))
		os.Exit(status)
	}
	os.Exit(m.Run())
}

func fixture(t *testing.T) (*supervisor, string) {
	t.Helper()
	root := t.TempDir()
	state := filepath.Join(root, "state")
	runtime := filepath.Join(root, "runtime")
	for _, path := range []string{state, runtime} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	lock, err := os.OpenFile(filepath.Join(runtime, "lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lock.Close() })
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	generation := filepath.Join(state, "candidate-test")
	if err := os.Mkdir(generation, 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(generation, "compose.json"), map[string]any{"services": map[string]any{"devshell": map[string]any{"container_name": "final-test-name"}}}); err != nil {
		t.Fatal(err)
	}
	self := &supervisor{root: root, project: "test-project", state: state, runtime: runtime, candidate: generation, shellJSON: `["old-shell","literal $value"]`, lock: lock, sessionID: "own-session"}
	toolPath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CDS_HELPER_TOOL", "1")
	tools := t.TempDir()
	for _, name := range []string{"podman", "docker-compose"} {
		if err := os.Symlink(toolPath, filepath.Join(tools, name)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	calls := filepath.Join(root, "calls")
	t.Setenv("CDS_CALLS", calls)
	return self, calls
}

func readCalls(t *testing.T, path string) []call {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var result []call
	decoder := json.NewDecoder(file)
	for {
		var entry call
		err := decoder.Decode(&entry)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, entry)
	}
	return result
}

func operations(calls []call) string {
	var result []string
	for _, entry := range calls {
		result = append(result, entry.Operation)
	}
	return strings.Join(result, ",")
}

func TestFinalDocumentNameAndSnapshotReuse(t *testing.T) {
	s, path := fixture(t)
	// The shell argv belongs to the active state, with no launcher config snapshot.
	code, err := s.enter(nil)
	if err != nil || code != 0 {
		t.Fatalf("enter: %d %v", code, err)
	}
	calls := readCalls(t, path)
	if operations(calls) != "up,exec,down" {
		t.Fatal("unexpected lifecycle:", operations(calls))
	}
	if strings.Join(calls[1].Arguments[len(calls[1].Arguments)-3:], "|") != "final-test-name|old-shell|literal $value" {
		t.Fatal("name and argv must come from active document/state")
	}
	if calls[0].Document != calls[2].Document || calls[0].Document == "" {
		t.Fatal("up/down did not receive the same document over stdin")
	}
	if _, err := os.Stat(s.candidate); !os.IsNotExist(err) {
		t.Fatal("successful down retained runtime environment")
	}
}

func TestDownFailurePreservesOriginalStatusAndGeneration(t *testing.T) {
	s, path := fixture(t)
	t.Setenv("CDS_STATUS_EXEC", "17")
	t.Setenv("CDS_STATUS_DOWN", "47")
	code, err := s.enter(nil)
	if err != nil || code != 17 {
		t.Fatalf("cleanup replaced original status: %d %v", code, err)
	}
	if _, err := s.loadActive(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.candidate, "compose.json")); err != nil {
		t.Fatal("failed down removed active generation")
	}
	t.Setenv("CDS_STATUS_DOWN", "0")
	if err := s.cleanup(true); err != nil {
		t.Fatal(err)
	}
	if operations(readCalls(t, path)) != "up,exec,down,down" {
		t.Fatal("manual retry did not use retained state")
	}
}

func TestConcurrentEntryUsesActiveVersion(t *testing.T) {
	s, path := fixture(t)
	value := active{ComposeFile: filepath.Join(s.candidate, "compose.json"), Shell: []string{"old-shell", "literal $value"}, GenerationDir: s.candidate}
	if err := writeJSON(filepath.Join(s.state, "active.json"), value); err != nil {
		t.Fatal(err)
	}
	other := map[string]session{"other": {PID: os.Getpid(), Start: processStart(os.Getpid())}}
	if err := writeJSON(filepath.Join(s.runtime, "sessions.json"), other); err != nil {
		t.Fatal(err)
	}
	s.candidate = ""
	s.shellJSON = "invalid-new-launcher-shell"
	code, err := s.enter(nil)
	if code != 0 || err != nil {
		t.Fatalf("concurrent enter: %d %v", code, err)
	}
	calls := readCalls(t, path)
	if strings.Join(calls[0].Arguments[len(calls[0].Arguments)-2:], "|") != "old-shell|literal $value" {
		t.Fatal("concurrent entry did not preserve active shell")
	}
	if operations(calls) != "exec" {
		t.Fatal("concurrent shell prepared or stopped shared resources")
	}
	if err := s.cleanup(true); err == nil {
		t.Fatal("manual cleanup accepted live sessions")
	}
}

func TestSignalDuringExecPreservesStatusAndCleansOnce(t *testing.T) {
	s, path := fixture(t)
	t.Setenv("CDS_HOLD_EXEC", "1")
	result := make(chan int, 1)
	go func() {
		code, err := s.enter(nil)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		result <- code
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		data, _ := os.ReadFile(path)
		if strings.Contains(string(data), `"Operation":"exec"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("exec did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.handleSignal(syscall.SIGTERM)
	select {
	case code := <-result:
		if code != 143 {
			t.Fatalf("signal status: %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("supervisor did not finish after TERM")
	}
	if operations(readCalls(t, path)) != "up,exec,down" {
		t.Fatal("signal cleanup was not exactly once")
	}
}

func TestFailedUpRetiresSessionAndGeneration(t *testing.T) {
	s, path := fixture(t)
	t.Setenv("CDS_STATUS_UP", "23")
	code, err := s.enter(nil)
	if err != nil || code != 23 {
		t.Fatalf("up failure status: %d %v", code, err)
	}
	if operations(readCalls(t, path)) != "up,down" {
		t.Fatal("up failure must clean up without exec")
	}
	if value, err := s.loadActive(); err != nil || value != nil {
		t.Fatal("successful failed-up cleanup retained active state")
	}
	entries, err := s.sessions()
	if err != nil || len(entries) != 0 {
		t.Fatal("failed startup leaked session")
	}
	if _, err := os.Stat(s.candidate); !os.IsNotExist(err) {
		t.Fatal("failed-up candidate leaked")
	}
}

func TestStaleDownFailureRetainsOldGenerationOnly(t *testing.T) {
	s, path := fixture(t)
	old := filepath.Join(s.state, "old-generation")
	if err := os.Mkdir(old, 0700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(s.candidate, "compose.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "compose.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	value := active{ComposeFile: filepath.Join(old, "compose.json"), Shell: []string{"old-shell", "literal $value"}, GenerationDir: old}
	if err := writeJSON(filepath.Join(s.state, "active.json"), value); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CDS_STATUS_DOWN", "47")
	code, err := s.enter(nil)
	if err != nil || code != 47 {
		t.Fatalf("stale cleanup status: %d %v", code, err)
	}
	if operations(readCalls(t, path)) != "down" {
		t.Fatal("new version started after failed stale cleanup")
	}
	retained, err := s.loadActive()
	if err != nil || retained.GenerationDir != old {
		t.Fatal("old active overwritten")
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatal("failed down removed old generation")
	}
	if _, err := os.Stat(s.candidate); !os.IsNotExist(err) {
		t.Fatal("unadopted candidate leaked after stale-down failure")
	}
}

func TestUnpublishFailureKeepsGenerationAndArtifacts(t *testing.T) {
	s, _ := fixture(t)
	value := active{GenerationDir: s.candidate}
	// An occupied directory makes unlink fail, even for privileged test users.
	manifest := filepath.Join(s.state, "active.json")
	if err := os.Mkdir(manifest, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manifest, "occupied"), []byte("marker"), 0600); err != nil {
		t.Fatal(err)
	}
	artifacts := filepath.Join(s.state, "artifacts")
	if err := os.WriteFile(artifacts, []byte("persistent-root"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.removeGeneration(&value); err == nil {
		t.Fatal("unlink failure ignored")
	}
	if _, err := os.Stat(filepath.Join(s.candidate, "compose.json")); err != nil {
		t.Fatal("generation removed despite failing to unpublish")
	}
	data, err := os.ReadFile(artifacts)
	if err != nil || string(data) != "persistent-root" {
		t.Fatal("persistent artifacts modified")
	}
}

func TestGenerationRemovalFailureLeavesUnreservedCandidate(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses the directory permissions used to trigger removal failure")
	}
	s, _ := fixture(t)
	value := active{GenerationDir: s.candidate}
	if err := writeJSON(filepath.Join(s.state, "active.json"), value); err != nil {
		t.Fatal(err)
	}
	artifacts := filepath.Join(s.state, "artifacts")
	if err := os.WriteFile(artifacts, []byte("persistent-root"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(s.candidate, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(s.candidate, 0700) })
	if err := s.removeGeneration(&value); err == nil {
		t.Fatal("removal failure ignored")
	}
	if _, err := os.Stat(filepath.Join(s.state, "active.json")); !os.IsNotExist(err) {
		t.Fatal("active pointer still published after down")
	}
	if _, err := os.Stat(s.candidate); err != nil {
		t.Fatal("failed removal lost the orphan path")
	}
	data, err := os.ReadFile(artifacts)
	if err != nil || string(data) != "persistent-root" {
		t.Fatal("persistent root modified")
	}
	// Restoring ordinary permissions makes this unreserved candidate removable.
	if err := os.Chmod(s.candidate, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(s.candidate); err != nil {
		t.Fatal(err)
	}
}

func TestMissingComposeOnPATHPreservesActive(t *testing.T) {
	s, _ := fixture(t)
	value := active{ComposeFile: filepath.Join(s.candidate, "compose.json"), GenerationDir: s.candidate, Shell: []string{"old-shell"}}
	if err := writeJSON(filepath.Join(s.state, "active.json"), value); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	if err := s.cleanup(true); err == nil {
		t.Fatal("missing Compose executable ignored")
	}
	retained, err := s.loadActive()
	if err != nil || retained == nil {
		t.Fatal("missing provider lost active state")
	}
	if _, err := os.Stat(value.ComposeFile); err != nil {
		t.Fatal("missing provider removed snapshot")
	}
}

func TestAdmissionRejectsInvalidShellBeforeDown(t *testing.T) {
	for _, shell := range []string{"", "null", "[]", `[""]`, `[1]`, `["shell",null]`, `{}`} {
		t.Run(shell, func(t *testing.T) {
			s, calls := fixture(t)
			old := active{ComposeFile: filepath.Join(s.candidate, "compose.json"), GenerationDir: filepath.Join(s.state, "candidate-old"), Shell: []string{"old-shell"}}
			if err := writeJSON(filepath.Join(s.state, "active.json"), old); err != nil {
				t.Fatal(err)
			}
			s.shellJSON = shell
			code, err := s.enter(nil)
			if code != 1 || err == nil {
				t.Fatalf("invalid shell accepted: %d %v", code, err)
			}
			if _, err := os.Stat(calls); !os.IsNotExist(err) {
				t.Fatal("invalid shell triggered Compose")
			}
			retained, err := s.loadActive()
			if err != nil || retained == nil || retained.GenerationDir != old.GenerationDir {
				t.Fatal("invalid shell changed active state")
			}
		})
	}
}
