// Linux host-side session supervision; Compose remains responsible for services and volumes.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

type active struct {
	ComposeFile   string   `json:"composeFile"`
	Shell         []string `json:"shell"`
	GenerationDir string   `json:"generationDir"`
}

type session struct {
	PID   int    `json:"pid"`
	Start string `json:"start"`
}

type supervisor struct {
	root, project, state, runtime, candidate, shellJSON string
	lock                                                *os.File
	registered                                          bool
	sessionID                                           string
	mu                                                  sync.Mutex
	child                                               *exec.Cmd
	pending                                             syscall.Signal
}

func readJSON(path string, output any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, output)
}

func writeJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".state-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func processStart(pid int) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return ""
	}
	end := strings.LastIndex(string(data), ")")
	if end == -1 {
		return ""
	}
	fields := strings.Fields(string(data)[end+1:])
	if len(fields) < 20 || fields[0] == "Z" || fields[0] == "X" {
		return ""
	}
	return fields[19]
}

func terminal(file *os.File) bool {
	var state syscall.Termios
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&state)))
	return err == 0
}

func (s *supervisor) handleSignal(value os.Signal) {
	signum := value.(syscall.Signal)
	s.mu.Lock()
	defer s.mu.Unlock()
	if signum == syscall.SIGINT && s.child != nil && terminal(os.Stdin) {
		return
	}
	s.pending = signum
	if s.child != nil {
		_ = s.child.Process.Signal(signum)
	}
}

func (s *supervisor) run(argv []string, input io.Reader, quiet bool) (int, error) {
	s.mu.Lock()
	if s.pending != 0 {
		code := 128 + int(s.pending)
		s.mu.Unlock()
		return code, nil
	}
	command := exec.Command(argv[0], argv[1:]...)
	command.Dir = s.root
	command.Stdin = input
	command.Stderr = os.Stderr
	if quiet {
		command.Stdout = io.Discard
	} else {
		command.Stdout = os.Stdout
	}
	// Inherit the terminal's process group. The flock descriptor is not in ExtraFiles.
	if err := command.Start(); err != nil {
		s.mu.Unlock()
		return 1, err
	}
	s.child = command
	s.mu.Unlock()
	err := command.Wait()
	s.mu.Lock()
	s.child = nil
	pending := s.pending
	s.mu.Unlock()
	if pending != 0 {
		return 128 + int(pending), nil
	}
	if err == nil {
		return 0, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		status := exit.Sys().(syscall.WaitStatus)
		if status.Signaled() {
			return 128 + int(status.Signal()), nil
		}
		return exit.ExitCode(), nil
	}
	return 1, err
}

func (s *supervisor) loadActive() (*active, error) {
	var value active
	err := readJSON(filepath.Join(s.state, "active.json"), &value)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return &value, err
}

func (s *supervisor) sessions() (map[string]session, error) {
	result := map[string]session{}
	err := readJSON(filepath.Join(s.runtime, "sessions.json"), &result)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if result == nil {
		result = map[string]session{}
	}
	for id, entry := range result {
		if entry.Start == "" || processStart(entry.PID) != entry.Start {
			delete(result, id)
		}
	}
	return result, nil
}

func (s *supervisor) compose(value *active, arguments ...string) (int, error) {
	input, err := os.Open(value.ComposeFile)
	if err != nil {
		return 1, err
	}
	defer input.Close()
	return s.run(append([]string{"docker-compose", "-p", s.project, "-f", "-"}, arguments...), input, true)
}

func (s *supervisor) validateGeneration(path string) error {
	// Delete only a generation below this project's state, never an arbitrary path.
	relative, err := filepath.Rel(s.state, path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("active generation must be below the project's state directory")
	}
	return nil
}

func (s *supervisor) removeGeneration(value *active) error {
	if err := s.validateGeneration(value.GenerationDir); err != nil {
		return err
	}
	// Unpublish first: interrupted removal leaves an unreserved candidate which
	// the next orchestrator can clean, never an active pointer to missing files.
	if err := os.Remove(filepath.Join(s.state, "active.json")); err != nil {
		return err
	}
	return os.RemoveAll(value.GenerationDir)
}

func (s *supervisor) down(value *active) (int, error) {
	if value == nil {
		return 0, nil
	}
	code, err := s.compose(value, "down")
	if err != nil || code != 0 {
		return code, err
	}
	return 0, s.removeGeneration(value)
}

func containerName(path string) (string, error) {
	var document struct {
		Services map[string]struct {
			Name string `json:"container_name"`
		} `json:"services"`
	}
	if err := readJSON(path, &document); err != nil {
		return "", err
	}
	name := document.Services["devshell"].Name
	if name == "" || strings.Contains(name, "$") {
		return "", fmt.Errorf("runtime Compose must contain a literal devshell.container_name")
	}
	return name, nil
}

func (s *supervisor) enter(command []string) (code int, err error) {
	adopted := false
	if s.candidate != "" {
		if err := s.validateGeneration(s.candidate); err != nil {
			return 1, err
		}
		defer func() {
			if !adopted {
				if cleanupErr := os.RemoveAll(s.candidate); cleanupErr != nil {
					fmt.Fprintln(os.Stderr, "supervisor: could not remove unadopted candidate:", cleanupErr)
				}
			}
		}()
	}
	defer func() {
		if cleanupErr := s.cleanup(false); cleanupErr != nil {
			fmt.Fprintln(os.Stderr, "supervisor: cleanup incomplete:", cleanupErr)
		}
	}()
	value, code, err := s.admit(&adopted)
	if err != nil || code != 0 {
		return code, err
	}
	if err := syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN); err != nil {
		return 1, err
	}
	return s.executeSession(value, command)
}

// admit prepares and registers the session while the inherited project lock is held.
func (s *supervisor) admit(adopted *bool) (*active, int, error) {
	entries, err := s.sessions()
	if err != nil {
		return nil, 1, err
	}
	value, err := s.loadActive()
	if err != nil {
		return nil, 1, err
	}
	if value != nil && s.candidate != "" && filepath.Clean(value.GenerationDir) == filepath.Clean(s.candidate) {
		*adopted = true // Never delete an existing active generation as an unused candidate.
		return nil, 1, fmt.Errorf("candidate must be a new generation, not the active generation")
	}
	if len(entries) == 0 {
		var argv []any
		if err := json.Unmarshal([]byte(s.shellJSON), &argv); err != nil || len(argv) == 0 {
			return nil, 1, fmt.Errorf("shell-json must be a nonempty string array")
		}
		shell := make([]string, len(argv))
		for index, argument := range argv {
			text, ok := argument.(string)
			if !ok || (index == 0 && text == "") {
				return nil, 1, fmt.Errorf("shell-json must contain strings and a nonempty executable")
			}
			shell[index] = text
		}
		if code, err := s.down(value); err != nil || code != 0 {
			return nil, code, err
		}
		if s.candidate == "" {
			return nil, 1, fmt.Errorf("a candidate generation is required for an inactive project")
		}
		value = &active{ComposeFile: filepath.Join(s.candidate, "compose.json"), Shell: shell, GenerationDir: s.candidate}
		if _, err := containerName(value.ComposeFile); err != nil {
			return nil, 1, err
		}
		if err := writeJSON(filepath.Join(s.state, "active.json"), value); err != nil {
			return nil, 1, err
		}
		*adopted = true
	} else if value == nil {
		return nil, 1, fmt.Errorf("live sessions have no active generation")
	}
	entries[s.sessionID] = session{PID: os.Getpid(), Start: processStart(os.Getpid())}
	if err := writeJSON(filepath.Join(s.runtime, "sessions.json"), entries); err != nil {
		return nil, 1, err
	}
	s.registered = true
	if len(entries) == 1 {
		if code, err := s.compose(value, "up", "-d", "devshell"); err != nil || code != 0 {
			return nil, code, err
		}
	}
	return value, 0, nil
}

// executeSession runs after admission has released the project lock.
func (s *supervisor) executeSession(value *active, command []string) (int, error) {
	name, err := containerName(value.ComposeFile)
	if err != nil {
		return 1, err
	}
	if len(command) == 0 {
		command = value.Shell
	}
	arguments := []string{"podman", "exec", "--interactive"}
	if terminal(os.Stdin) && terminal(os.Stdout) {
		arguments = append(arguments, "--tty")
	}
	if term, exists := os.LookupEnv("TERM"); exists {
		arguments = append(arguments, "-e", "TERM="+term)
	}
	arguments = append(arguments, name)
	arguments = append(arguments, command...)
	return s.run(arguments, os.Stdin, false)
}

func (s *supervisor) cleanup(manual bool) error {
	if !manual && !s.registered {
		return nil
	}
	s.registered = false
	s.mu.Lock()
	s.pending = 0
	s.mu.Unlock()
	if err := syscall.Flock(int(s.lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	entries, err := s.sessions()
	if err != nil {
		return err
	}
	delete(entries, s.sessionID)
	if err := writeJSON(filepath.Join(s.runtime, "sessions.json"), entries); err != nil {
		return err
	}
	if len(entries) != 0 {
		if manual {
			return fmt.Errorf("live sessions prevent cleanup")
		}
		return nil
	}
	value, err := s.loadActive()
	if err != nil {
		return err
	}
	code, err := s.down(value)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("Compose down exited %d", code)
	}
	return nil
}

func main() {
	var s supervisor
	flag.StringVar(&s.root, "project-root", "", "canonical project directory")
	flag.StringVar(&s.project, "project", "", "Compose identity")
	flag.StringVar(&s.state, "state-dir", "", "persistent project state")
	flag.StringVar(&s.runtime, "runtime-dir", "", "temporary session registry")
	flag.StringVar(&s.candidate, "candidate", "", "prepared generation directory")
	flag.StringVar(&s.shellJSON, "shell-json", "", "shell argv JSON for a new generation")
	fd := flag.Int("lock-fd", -1, "inherited, already locked project descriptor")
	manual := flag.Bool("cleanup", false, "clean abandoned services")
	flag.Parse()
	if *fd < 0 || s.root == "" || s.state == "" || s.runtime == "" || s.project == "" {
		fmt.Fprintln(os.Stderr, "supervisor: project-root, project, state-dir, runtime-dir and lock-fd are required")
		os.Exit(1)
	}
	s.lock = os.NewFile(uintptr(*fd), "project-lock")
	syscall.CloseOnExec(*fd)
	s.sessionID = strconv.Itoa(os.Getpid()) + "-" + processStart(os.Getpid())
	defer s.lock.Close()
	changes := make(chan os.Signal, 8)
	signal.Notify(changes, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		for value := range changes {
			s.handleSignal(value)
		}
	}()
	var code int
	var err error
	if *manual {
		err = s.cleanup(true)
	} else {
		code, err = s.enter(flag.Args())
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "supervisor:", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}
