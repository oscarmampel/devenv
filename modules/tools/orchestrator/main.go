package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

type session struct {
	PID   int    `json:"pid"`
	Start string `json:"start"`
}

type active struct {
	GenerationDir string `json:"generationDir"`
}

var invalidProjectChar = regexp.MustCompile(`[^a-z0-9_-]+`)

func projectIdentity(root string) string {
	name := strings.Trim(invalidProjectChar.ReplaceAllString(strings.ToLower(filepath.Base(root)), "-"), "-_")
	if name == "" {
		name = "project"
	}
	if len(name) > 32 {
		name = name[:32]
	}
	hash := sha256.Sum256([]byte(root))
	return "devshell-" + name + "-" + hex.EncodeToString(hash[:])[:12]
}

func privateDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	return os.Chmod(path, 0700)
}

func startTime(pid int) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return ""
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return ""
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) <= 19 || fields[0] == "Z" || fields[0] == "X" {
		return ""
	}
	return fields[19]
}

func liveSessions(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var entries map[string]session
	if err := json.Unmarshal(data, &entries); err != nil {
		return false, fmt.Errorf("invalid session registry: %w", err)
	}
	for _, entry := range entries {
		if entry.Start != "" && startTime(entry.PID) == entry.Start {
			return true, nil
		}
	}
	return false, nil
}

func cleanupAbandonedCandidates(state, activePath string) error {
	protected := ""
	if data, err := os.ReadFile(activePath); err == nil {
		var current active
		if err := json.Unmarshal(data, &current); err != nil {
			return fmt.Errorf("invalid active configuration: %w", err)
		}
		if current.GenerationDir == "" {
			return errors.New("active configuration has no generation directory")
		}
		relative, err := filepath.Rel(state, current.GenerationDir)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			return errors.New("active generation must be below the project state directory")
		}
		protected = filepath.Clean(current.GenerationDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	candidates, err := filepath.Glob(filepath.Join(state, "candidate-*"))
	if err != nil {
		return err
	}
	for _, path := range candidates {
		if filepath.Clean(path) == protected {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	return nil
}

func run(root, program string, args ...string) error {
	command := exec.Command(program, args...)
	command.Dir = root
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

func ensureImage(root, name, archive string) error {
	check := exec.Command("podman", "image", "exists", name)
	check.Dir = root
	check.Stdout = io.Discard
	check.Stderr = os.Stderr
	err := check.Run()
	if err == nil {
		return nil
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		return fmt.Errorf("checking Podman image: %w", err)
	}
	if err := run(root, "podman", "load", "-i", archive); err != nil {
		return fmt.Errorf("loading Podman image: %w", err)
	}
	return nil
}

func cleanupPrevious(root, project, state, runtime, supervisorPath string, lock *os.File) error {
	command := exec.Command(supervisorPath,
		"--project-root", root,
		"--project", project,
		"--state-dir", state,
		"--runtime-dir", runtime,
		"--lock-fd", "3",
		"--cleanup")
	command.Dir = root
	command.Env = os.Environ()
	command.ExtraFiles = []*os.File{lock}
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("failed to clean up previous generation; active state and GC roots were retained: %w", err)
	}
	return nil
}

func copyPrivate(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func imageNameFromServices(path string) (string, error) {
	var document struct {
		Services map[string]struct {
			Image string `json:"image"`
		} `json:"services"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return "", err
	}
	name := document.Services["devshell"].Image
	if name == "" {
		return "", errors.New("services.devshell.image must be a nonempty string")
	}
	return name, nil
}

func candidate(root, state, project, servicesFile, imageArchive, artifactRoot, innerDerivation string) (string, error) {
	directory, err := os.MkdirTemp(state, "candidate-")
	if err != nil {
		return "", err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(directory)
		}
	}()
	if err := os.Chmod(directory, 0700); err != nil {
		return "", err
	}
	if err := copyPrivate(servicesFile, filepath.Join(directory, "services.json")); err != nil {
		return "", err
	}
	imageName, err := imageNameFromServices(filepath.Join(directory, "services.json"))
	if err != nil {
		return "", err
	}
	if err := run(root, "nix", "build", "--out-link", filepath.Join(state, "artifacts"), artifactRoot); err != nil {
		return "", fmt.Errorf("protecting Nix artifacts: %w", err)
	}
	captureArgs := []string{"--project-root", root, "--state-dir", state, "--output", filepath.Join(directory, "environment.json"), "--derivation", innerDerivation}
	if err := run(root, "capture-env", captureArgs...); err != nil {
		return "", fmt.Errorf("capturing shell environment: %w", err)
	}
	if err := run(root, "compose-builder",
		"--services", filepath.Join(directory, "services.json"),
		"--captured", filepath.Join(directory, "environment.json"),
		"--project", project,
		"--output", filepath.Join(directory, "compose.json")); err != nil {
		return "", fmt.Errorf("building Compose document: %w", err)
	}
	if err := ensureImage(root, imageName, imageArchive); err != nil {
		return "", err
	}
	complete = true
	return directory, nil
}

func runMain() error {
	servicesFile := flag.String("services", "", "Nix-generated static Compose services")
	imageArchive := flag.String("image-archive", "", "Nix-generated image tar")
	artifactRoot := flag.String("artifact-root", "", "Nix-generated GC root target")
	innerDerivation := flag.String("inner-derivation", "", "Nix derivation for the inner shell")
	shellJSON := flag.String("shell-json", "", "Nix-generated shell argv for new generations")
	cleanup := flag.Bool("cleanup", false, "clean an inactive project")
	flag.Parse()
	if *servicesFile == "" || *imageArchive == "" || *artifactRoot == "" || *innerDerivation == "" || *shellJSON == "" {
		return errors.New("services, image-archive, artifact-root, inner-derivation and shell-json are required")
	}
	supervisorPath, err := exec.LookPath("supervisor")
	if err != nil {
		return fmt.Errorf("finding supervisor on PATH: %w", err)
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	if strings.Contains(root, ":") {
		return errors.New("Compose does not support ':' in project paths")
	}
	project := projectIdentity(root)
	stateBase := os.Getenv("XDG_STATE_HOME")
	if stateBase == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		stateBase = filepath.Join(home, ".local", "state")
	}
	runtimeBase := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeBase == "" {
		return errors.New("XDG_RUNTIME_DIR is required")
	}
	info, err := os.Stat(runtimeBase)
	if err != nil || !info.IsDir() {
		return errors.New("XDG_RUNTIME_DIR must be an existing directory")
	}
	if owner, ok := info.Sys().(*syscall.Stat_t); !ok || owner.Uid != uint32(os.Getuid()) {
		return errors.New("XDG_RUNTIME_DIR must belong to the current user")
	}
	state := filepath.Join(stateBase, "shell-sandbox", project)
	runtime := filepath.Join(runtimeBase, "shell-sandbox", project)
	if err := privateDirectory(state); err != nil {
		return err
	}
	if err := privateDirectory(runtime); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(runtime, "lock"), os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	active := filepath.Join(state, "active.json")
	_, activeError := os.Stat(active)
	if activeError != nil && !errors.Is(activeError, os.ErrNotExist) {
		return activeError
	}
	live, err := liveSessions(filepath.Join(runtime, "sessions.json"))
	if err != nil {
		return err
	}
	if live && errors.Is(activeError, os.ErrNotExist) {
		return errors.New("live sessions exist without active configuration")
	}
	if !live && activeError == nil && !*cleanup {
		if err := cleanupPrevious(root, project, state, runtime, supervisorPath, lock); err != nil {
			return err
		}
		if _, err := os.Stat(active); !errors.Is(err, os.ErrNotExist) {
			if err == nil {
				return errors.New("previous generation cleanup returned successfully but active state remains")
			}
			return err
		}
	}
	if !live {
		if err := cleanupAbandonedCandidates(state, active); err != nil {
			return err
		}
	}
	newCandidate := ""
	if !*cleanup && !live {
		newCandidate, err = candidate(root, state, project, *servicesFile, *imageArchive, *artifactRoot, *innerDerivation)
		if err != nil {
			return err
		}
		// Exec replaces this process on success. If handoff fails, no generation
		// has been published and its private files can be removed.
		defer os.RemoveAll(newCandidate)
	}
	// Preserve the lock across exec so the supervisor can register the session
	// and complete up/down before another orchestrator enters the project.
	fd := int(lock.Fd())
	_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_SETFD, 0)
	if errno != 0 {
		return errno
	}
	args := []string{supervisorPath,
		"--project-root", root,
		"--project", project,
		"--state-dir", state,
		"--runtime-dir", runtime,
		"--lock-fd", strconv.Itoa(fd),
	}
	if *cleanup {
		args = append(args, "--cleanup")
	} else {
		if newCandidate != "" {
			args = append(args, "--candidate", newCandidate, "--shell-json", *shellJSON)
		}
		args = append(args, "--")
		if remaining := flag.Args(); len(remaining) > 0 {
			if remaining[0] == "--" {
				remaining = remaining[1:]
			}
			args = append(args, remaining...)
		}
	}
	return syscall.Exec(supervisorPath, args, os.Environ())
}

func main() {
	if err := runMain(); err != nil {
		fmt.Fprintln(os.Stderr, "shell-sandbox:", err)
		os.Exit(1)
	}
}
