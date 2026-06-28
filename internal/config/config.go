// Package config handles SandboxMCP configuration loading.
// 設定ファイル、環境変数、CLIフラグの優先順位で設定を解決します。
//
// Priority (highest to lowest):
//   CLI flags > config file > environment variables > defaults
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config holds SandboxMCP configuration.
type Config struct {
	ScriptsDir  string
	ToolsDir    string
	UpdateCheck bool
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		ScriptsDir:  ".sandbox/scripts",
		ToolsDir:    ".sandbox/tools",
		UpdateCheck: true,
	}
}

// LoadFile loads configuration from a YAML file.
// Returns defaults if the file does not exist.
// ファイルが存在しない場合はデフォルト値を返します。
func LoadFile(path string) (Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("failed to read config: %w", err)
	}

	if len(strings.TrimSpace(string(data))) == 0 {
		return cfg, nil
	}

	parsed, err := parseSimpleYAML(data)
	if err != nil {
		return cfg, fmt.Errorf("failed to parse config: %w", err)
	}

	if v, ok := parsed["scripts_dir"]; ok {
		cfg.ScriptsDir = v
	}
	if v, ok := parsed["tools_dir"]; ok {
		cfg.ToolsDir = v
	}
	if v, ok := parsed["update_check"]; ok {
		cfg.UpdateCheck = (v == "true")
	}

	return cfg, nil
}

// LoadWithEnv applies environment variable overrides to a config.
// 環境変数による上書きを適用します。
func LoadWithEnv(cfg Config) Config {
	if v := os.Getenv("SANDBOX_SCRIPTS_DIR"); v != "" {
		cfg.ScriptsDir = v
	}
	if v := os.Getenv("SANDBOX_TOOLS_DIR"); v != "" {
		cfg.ToolsDir = v
	}
	return cfg
}

// FindConfigFile searches for sandbox-mcp.yaml in standard locations.
// Returns the path if found, or empty string if not found.
func FindConfigFile(workspaceDir string) string {
	// 1. .sandbox/config/sandbox-mcp.yaml
	candidate := filepath.Join(workspaceDir, ".sandbox", "config", "sandbox-mcp.yaml")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}

	// 2. ~/.config/sandbox-mcp/config.yaml
	home, err := os.UserHomeDir()
	if err == nil {
		candidate = filepath.Join(home, ".config", "sandbox-mcp", "config.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	return ""
}

// Resolve loads config with full priority chain.
// CLI flag values should be passed as flagScriptsDir and flagToolsDir
// (empty string means "not specified").
// workspace must be an absolute path (callers should use filepath.Abs before
// calling); when non-empty, relative ScriptsDir/ToolsDir values are joined
// against it to produce absolute paths.
//
// Priority: CLI flags > config file > env vars > defaults
//
// If configFile is specified but cannot be parsed, the error is returned along
// with the config resolved from lower-priority layers (env vars and defaults).
// Callers should log a warning and continue rather than aborting.
func Resolve(configFile, flagScriptsDir, flagToolsDir, workspace string) (Config, error) {
	// Start with defaults
	cfg := DefaultConfig()

	// Layer 1: environment variables
	cfg = LoadWithEnv(cfg)

	// Layer 2: config file (overrides only the keys present in the file)
	var cfgErr error
	if configFile != "" {
		merged, err := mergeFileConfig(cfg, configFile)
		if err != nil {
			cfgErr = err
		} else {
			cfg = merged
		}
	}

	// Layer 3: CLI flags (highest priority)
	if flagScriptsDir != "" {
		cfg.ScriptsDir = flagScriptsDir
	}
	if flagToolsDir != "" {
		cfg.ToolsDir = flagToolsDir
	}

	// Layer 4: resolve relative paths against workspace
	// workspace が指定されていれば相対パスを絶対パスに解決
	if workspace != "" {
		if !filepath.IsAbs(cfg.ScriptsDir) {
			cfg.ScriptsDir = filepath.Join(workspace, cfg.ScriptsDir)
		}
		if !filepath.IsAbs(cfg.ToolsDir) {
			cfg.ToolsDir = filepath.Join(workspace, cfg.ToolsDir)
		}
	}

	return cfg, cfgErr
}

// mergeFileConfig reads a config file and applies only the keys
// present in the file onto the base config, preserving values from
// lower-priority layers for keys not specified in the file.
func mergeFileConfig(base Config, path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return base, nil
		}
		return base, fmt.Errorf("failed to read config: %w", err)
	}

	if len(strings.TrimSpace(string(data))) == 0 {
		return base, nil
	}

	parsed, err := parseSimpleYAML(data)
	if err != nil {
		return base, fmt.Errorf("failed to parse config: %w", err)
	}

	if v, ok := parsed["scripts_dir"]; ok {
		base.ScriptsDir = v
	}
	if v, ok := parsed["tools_dir"]; ok {
		base.ToolsDir = v
	}
	if v, ok := parsed["update_check"]; ok {
		base.UpdateCheck = (v == "true")
	}

	return base, nil
}

// parseSimpleYAML parses a minimal subset of YAML (key: value pairs only).
// No external dependencies needed.
func parseSimpleYAML(data []byte) (map[string]string, error) {
	result := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid line: %s", line)
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Strip inline comments (e.g. "value  # comment" → "value")
		if idx := strings.Index(value, " #"); idx >= 0 {
			value = strings.TrimSpace(value[:idx])
		}
		value = strings.Trim(value, `"'`)

		result[key] = value
	}

	return result, scanner.Err()
}
