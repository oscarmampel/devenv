package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

func main() {
	root := flag.String("project-root", "", "Host project root")
	output := flag.String("output", "", "Private JSON output")
	state := flag.String("state-dir", "", "Private state directory")
	target := flag.String("derivation", "", "Inner shell derivation")
	captureCommand := flag.String("capture-command", "", "Bash environment capture helper")
	flag.Parse()
	if *root == "" || *output == "" || *state == "" || *target == "" || *captureCommand == "" {
		fmt.Fprintln(os.Stderr, "capture-env: project root, output, state, derivation and capture command required")
		os.Exit(2)
	}
	host := parseEnvironment([]byte(strings.Join(os.Environ(), "\x00")))
	delta, err := os.CreateTemp(*state, ".environment-delta-*")
	if err != nil {
		fail(err)
	}
	deltaPath := delta.Name()
	defer os.Remove(deltaPath)
	if err := delta.Close(); err != nil {
		fail(err)
	}
	cmd := exec.Command(*captureCommand,
		"--project-root", *root,
		"--state-dir", *state,
		"--output", deltaPath,
		"--derivation", *target)
	cmd.Dir = *root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fail(err)
	}
	input, err := os.ReadFile(deltaPath)
	if err != nil {
		fail(err)
	}
	result, err := filterEnvironment(input, *root, host, os.Getenv("CDS_CAPTURE_PATH_SPEC"))
	if err != nil {
		fail(err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		fail(err)
	}
	file, err := os.CreateTemp(filepath.Dir(*output), ".environment-*")
	if err != nil {
		fail(err)
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(data); err != nil {
		file.Close()
		fail(err)
	}
	if err = file.Close(); err != nil {
		fail(err)
	}
	if err = os.Rename(name, *output); err != nil {
		fail(err)
	}
}

func fail(err error) { fmt.Fprintln(os.Stderr, "capture-env:", err); os.Exit(1) }

func parseEnvironment(input []byte) map[string]string {
	raw := map[string]string{}
	for _, entry := range strings.Split(string(input), "\x00") {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			raw[key] = value
		}
	}
	return raw
}

var hostReference = regexp.MustCompile(`#\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func interpolateHost(value string, host map[string]string) (string, error) {
	var missing string
	result := hostReference.ReplaceAllStringFunc(value, func(reference string) string {
		name := reference[2 : len(reference)-1]
		resolved, ok := host[name]
		if !ok {
			missing = name
			return reference
		}
		return resolved
	})
	if missing != "" {
		return "", fmt.Errorf("host environment variable %s is not set", missing)
	}
	return result, nil
}

func filterEnvironment(input []byte, root string, host map[string]string, pathSpec string) (map[string]string, error) {
	raw := parseEnvironment(input)
	if hostReference.MatchString(pathSpec) {
		if raw["PATH"] == "" {
			raw["PATH"] = pathSpec
		} else if !strings.Contains(raw["PATH"], pathSpec) {
			raw["PATH"] += ":" + pathSpec
		}
	}
	result := map[string]string{}
	root = filepath.Clean(root)
	for key, value := range raw {
		var err error
		value, err = interpolateHost(value, host)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		value = translateProject(value, root)
		if key == "PATH" {
			paths := []string{}
			seen := map[string]bool{}
			for _, entry := range append(strings.Split(value, ":"), "/bin", "/usr/bin") {
				if !filepath.IsAbs(entry) {
					continue
				}
				path := filepath.Clean(entry)
				mounted := path == "/workspace" || strings.HasPrefix(path, "/workspace/") || strings.HasPrefix(path, "/nix/store/") || path == "/bin" || path == "/usr/bin"
				if !mounted {
					if resolved, err := filepath.EvalSymlinks(path); err == nil {
						path = resolved
						mounted = strings.HasPrefix(path, "/nix/store/")
					}
				}
				if mounted && !seen[path] {
					paths = append(paths, path)
					seen[path] = true
				}
			}
			value = strings.Join(paths, ":")
		}
		result[key] = value
	}
	return result, nil
}

func translateProject(value, root string) string {
	if root == "/" {
		return value
	}
	var result strings.Builder
	for start := 0; start < len(value); {
		index := strings.Index(value[start:], root)
		if index < 0 {
			result.WriteString(value[start:])
			break
		}
		index += start
		end := index + len(root)
		before := index == 0 || strings.ContainsRune(" \t\n:=;,'\"", rune(value[index-1]))
		after := end == len(value) || strings.ContainsRune("/ \t\n:;,'\"", rune(value[end]))
		result.WriteString(value[start:index])
		if before && after {
			result.WriteString("/workspace")
		} else {
			result.WriteString(root)
		}
		start = end
	}
	return result.String()
}
