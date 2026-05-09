package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const scaffoldTemplate = `version: 1

target:
  name: my-api
  base_url: https://staging.example.com
  schema: ./openapi.yaml
  auth:
    type: bearer
    env: API_TOKEN

strategies:
  - type: table
    file: ./tests/table.yaml

  - type: property
    operations: []
    invariants:
      - no_5xx
      - response_matches_schema
    config:
      max_examples: 100

report:
  formats: [console, json]
  output_dir: .bt/reports

safety:
  profile: safe
  deny_methods: [DELETE]
`

func Scaffold(path string, force bool) error {
	path = filepath.Clean(path)
	if !force {
		if _, err := os.Stat(path); err == nil {
			return errors.New("config file already exists; use --force to overwrite")
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat config path: %w", err)
		}
	}
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("could not create config directory: %w", err)
		}
	}
	if err := os.WriteFile(path, []byte(scaffoldTemplate), 0o644); err != nil {
		return fmt.Errorf("could not write config: %w", err)
	}
	return nil
}
