package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("CDS_FAKE_PODMAN") == "1" {
		call := strings.Join(os.Args[1:], " ")
		file, err := os.OpenFile(os.Getenv("CDS_IMAGE_CALLS"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			os.Exit(99)
		}
		_, _ = file.WriteString(call + "\n")
		_ = file.Close()
		if strings.HasPrefix(call, "image exists ") {
			status, _ := strconv.Atoi(os.Getenv("CDS_IMAGE_STATUS"))
			os.Exit(status)
		}
		status, _ := strconv.Atoi(os.Getenv("CDS_LOAD_STATUS"))
		os.Exit(status)
	}
	os.Exit(m.Run())
}

func TestImageLoadOnlyWhenMissing(t *testing.T) {
	for _, tc := range []struct {
		name, inspect, load, calls string
		wantErr                    bool
	}{
		{"present", "0", "0", "image exists test-image\n", false},
		{"missing", "1", "0", "image exists test-image\nload -i /archive.tar\n", false},
		{"inspection-error", "42", "0", "image exists test-image\n", true},
		{"load-error", "1", "23", "image exists test-image\nload -i /archive.tar\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			bin := t.TempDir()
			if err := os.Symlink(path, filepath.Join(bin, "podman")); err != nil {
				t.Fatal(err)
			}
			calls := filepath.Join(t.TempDir(), "calls")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("CDS_FAKE_PODMAN", "1")
			t.Setenv("CDS_IMAGE_CALLS", calls)
			t.Setenv("CDS_IMAGE_STATUS", tc.inspect)
			t.Setenv("CDS_LOAD_STATUS", tc.load)
			err = ensureImage(t.TempDir(), "test-image", "/archive.tar")
			if (err != nil) != tc.wantErr {
				t.Fatalf("image result: %v", err)
			}
			data, err := os.ReadFile(calls)
			if err != nil || string(data) != tc.calls {
				t.Fatalf("image calls: %q, %v", data, err)
			}
		})
	}
}

func TestImageNameComesFromStaticServices(t *testing.T) {
	services := filepath.Join(t.TempDir(), "services.json")
	if err := os.WriteFile(services, []byte(`{"services":{"devshell":{"image":"custom:tag"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	name, err := imageNameFromServices(services)
	if err != nil || name != "custom:tag" {
		t.Fatalf("static Compose image: %q, %v", name, err)
	}
	if err := os.WriteFile(services, []byte(`{"services":{"devshell":{}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := imageNameFromServices(services); err == nil {
		t.Fatal("missing devshell image accepted")
	}
}

func TestInterruptedGenerationRemovalKeepsArtifactRoot(t *testing.T) {
	state := t.TempDir()
	current := filepath.Join(state, "candidate-current")
	orphan := filepath.Join(state, "candidate-orphan")
	for _, path := range []string{current, orphan} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	artifact := filepath.Join(state, "artifacts")
	if err := os.Symlink("/nix/store/example", artifact); err != nil {
		t.Fatal(err)
	}
	activePath := filepath.Join(state, "active.json")
	data, err := json.Marshal(active{GenerationDir: current})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(activePath, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := cleanupAbandonedCandidates(state, activePath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(current); err != nil {
		t.Fatalf("active generation was removed: %v", err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan candidate was retained: %v", err)
	}

	// A successful down may unlink active.json and then be interrupted before
	// it removes the private generation. The next entry collects the orphan.
	if err := os.Remove(activePath); err != nil {
		t.Fatal(err)
	}
	if err := cleanupAbandonedCandidates(state, activePath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(current); !os.IsNotExist(err) {
		t.Fatalf("inactive generation was retained: %v", err)
	}
	if _, err := os.Lstat(artifact); err != nil {
		t.Fatalf("persistent GC root was removed: %v", err)
	}
}
