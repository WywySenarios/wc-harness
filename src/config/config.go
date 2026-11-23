// Package config resolves the harness settings from a dotenv file and the process environment.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strings"
)

type Config struct {
	BaseURL         string
	Model           string
	APIKey          string
	ReasoningEffort string
}

var requiredKeys = []string{"WC_LLM_BASE_URL", "WC_LLM_MODEL", "WC_LLM_API_KEY"}

// Load the configuration.
func Load(path string) (Config, error) {
	fileValues, err := parseFile(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return Config{}, err
		}
		fileValues = map[string]string{}
	}

	get := func(key string) string {
		if value := os.Getenv(key); value != "" {
			return value
		}
		return fileValues[key]
	}

	var missing []string
	for _, key := range requiredKeys {
		if get(key) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required key(s): %s (checked %s and the environment)", strings.Join(missing, ", "), path)
	}

	baseURL := strings.TrimRight(get("WC_LLM_BASE_URL"), "/")
	// Accept the full chat-completions endpoint as well as the API base.
	baseURL = strings.TrimSuffix(baseURL, "/chat/completions")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return Config{}, errors.New("WC_LLM_BASE_URL must be an absolute http(s) URL")
	}

	return Config{
		BaseURL:         baseURL,
		Model:           get("WC_LLM_MODEL"),
		APIKey:          get("WC_LLM_API_KEY"),
		ReasoningEffort: get("WC_LLM_REASONING_EFFORT"),
	}, nil
}

// parseFile reads KEY=VALUE lines. Blank lines and # comments are ignored;
// values are not interpolated and inline comments are not stripped.
func parseFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read dotenv: %w", err)
	}
	defer file.Close()

	values := make(map[string]string)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 4096), 64*1024)

	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("%s:%d: expected KEY=VALUE", path, lineNo)
		}
		key = strings.TrimSpace(key)
		if key == "" || strings.ContainsAny(key, " \t") {
			return nil, fmt.Errorf("%s:%d: invalid key", path, lineNo)
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 {
			if first, last := value[0], value[len(value)-1]; (first == '"' && last == '"') || (first == '\'' && last == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read dotenv: %w", err)
	}
	return values, nil
}
