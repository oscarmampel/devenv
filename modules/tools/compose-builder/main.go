// compose-builder writes a private JSON document, a subset of YAML accepted by Compose.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func readJSON(path string, output any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, output)
}

func environment(value any) (map[string]any, error) {
	if value == nil {
		return map[string]any{}, nil
	}
	if entries, ok := value.(map[string]any); ok {
		return entries, nil
	}
	if entries, ok := value.([]any); ok {
		result := map[string]any{}
		for _, entry := range entries {
			text, ok := entry.(string)
			if !ok {
				return nil, fmt.Errorf("environment list entries must be strings")
			}
			key, value, assigned := strings.Cut(text, "=")
			if assigned {
				result[key] = value
			} else {
				result[key] = nil
			}
		}
		return result, nil
	}
	return nil, fmt.Errorf("environment must be an object or string list")
}

func render(document map[string]any, captured map[string]string, project string) (string, error) {
	services, ok := document["services"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("services must be an object")
	}
	service, ok := services["devshell"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("services.devshell must be an object")
	}
	variables := map[string]any{}
	// Captured values are literal; explicit Compose values retain interpolation.
	for key, value := range captured {
		variables[key] = strings.ReplaceAll(value, "$", "$$")
	}
	explicit, err := environment(service["environment"])
	if err != nil {
		return "", err
	}
	for key, value := range explicit {
		variables[key] = value
	}
	service["environment"] = variables
	name := project + "-devshell"
	if configured, exists := service["container_name"]; exists {
		var valid bool
		name, valid = configured.(string)
		if !valid || name == "" {
			return "", fmt.Errorf("container_name must be a nonempty string")
		}
	}
	if strings.Contains(name, "$") {
		return "", fmt.Errorf("container_name must be literal")
	}
	service["container_name"] = name
	return name, nil
}

func writePrivate(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".compose-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func run() error {
	servicesPath := flag.String("services", "", "copied static services snapshot")
	capturedPath := flag.String("captured", "", "captured environment JSON")
	project := flag.String("project", "", "Compose project identity")
	output := flag.String("output", "", "private Compose JSON output")
	flag.Parse()
	if *servicesPath == "" || *capturedPath == "" || *project == "" || *output == "" {
		return fmt.Errorf("services, captured, project and output are required")
	}
	var document map[string]any
	var captured map[string]string
	if err := readJSON(*servicesPath, &document); err != nil {
		return err
	}
	if err := readJSON(*capturedPath, &captured); err != nil {
		return err
	}
	if _, err := render(document, captured, *project); err != nil {
		return err
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	return writePrivate(*output, data)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "compose-builder:", err)
		os.Exit(1)
	}
}
