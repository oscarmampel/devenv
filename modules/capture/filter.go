package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	root := flag.String("project-root", "", "Host project root")
	output := flag.String("output", "", "Private JSON output")
	baseline := flag.String("baseline", "", "NUL-delimited environment before loading Nix")
	flag.Parse()
	if *root == "" || *output == "" || *baseline == "" {
		fmt.Fprintln(os.Stderr, "envfilter: project root, output and baseline required")
		os.Exit(2)
	}
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		fail(err)
	}
	before, err := os.ReadFile(*baseline)
	if err != nil {
		fail(err)
	}
	result := filterEnvironment(before, input, *root)
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
func fail(err error) { fmt.Fprintln(os.Stderr, "envfilter:", err); os.Exit(1) }

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

func filterEnvironment(before, input []byte, root string) map[string]string {
	raw := parseEnvironment(input)
	baseline := parseEnvironment(before)
	result := map[string]string{}
	root = filepath.Clean(root)
	for key, value := range raw {
		if previous, present := baseline[key]; present && previous == value {
			continue
		}
		if key == "_" || key == "SHLVL" || key == "direnv" ||
			strings.HasPrefix(key, "CDS_") || strings.HasPrefix(key, "BASH_FUNC_") ||
			strings.HasPrefix(key, "NIX_DIRENV_") || strings.HasPrefix(key, "DIRENV_") {
			continue
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
				if mounted && !seen[path] {
					paths = append(paths, path)
					seen[path] = true
				}
			}
			value = strings.Join(paths, ":")
		}
		result[key] = value
	}
	return result
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
