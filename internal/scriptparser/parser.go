// Package scriptparser parses .sandbox/scripts/ shell script headers.
package scriptparser

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ScriptInfo holds parsed metadata about a script.
type ScriptInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Environment string `json:"environment"` // "container", "any"
	Category    string `json:"category"`    // "utility", "test"
	Advertise   bool   `json:"advertise,omitempty"`
	Hidden      bool   `json:"hidden,omitempty"`
	Usage       string `json:"usage,omitempty"`
	Options     string `json:"options,omitempty"`
}

// ListScripts returns metadata for all scripts in the directory.
func ListScripts(dir string) ([]ScriptInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading directory: %w", err)
	}

	var scripts []ScriptInfo
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		// Skip libraries (underscore prefix); there is no hardcoded filename
		// exclusion beyond this — scripts opt out of listing via @hidden: true.
		if strings.HasPrefix(name, "_") {
			continue
		}
		// No extension check: any executable file is a script, regardless of
		// language. This relies on the file's own shebang (executor.RunScript
		// invokes it directly rather than via bash) and works for any
		// language whose comments use "#" (Python, Ruby, Perl, shell, ...).
		// Use os.Stat (not e.Info(), which is Lstat-based) so a symlink's
		// executable bit is resolved from its target, matching executor.RunScript
		// — otherwise a symlink to a non-executable file would be listed here
		// but fail to run there.
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil || fi.Mode()&0111 == 0 {
			continue
		}

		info, err := parseHeader(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		if info.Hidden {
			continue
		}
		scripts = append(scripts, info)
	}
	return scripts, nil
}

// GetDetailedInfo returns full info including usage and options.
func GetDetailedInfo(dir, name string) (ScriptInfo, error) {
	if strings.Contains(name, "/") || name == ".." || strings.HasPrefix(name, "../") {
		return ScriptInfo{}, fmt.Errorf("invalid script name: %s", name)
	}

	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err != nil {
		return ScriptInfo{}, fmt.Errorf("script not found: %s", name)
	}

	return parseDetailedHeader(path)
}

// parseHeader extracts basic info from script header lines.
// Expected format:
//
//	Line 1: #!/bin/bash
//	Line 2: # filename.sh
//	Line 3+: # Description text (until # --- separator or end of comments)
//
// Parsing stops at:
//   - # --- separator (content after this is ignored, similar to Go tools' // ---)
//   - Non-comment line
//   - End of file
func parseHeader(path string) (ScriptInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return ScriptInfo{}, err
	}
	defer f.Close()

	name := filepath.Base(path)
	info := ScriptInfo{
		Name:        name,
		Environment: classifyEnvironment(name),
		Category:    classifyCategory(name),
	}

	scanner := bufio.NewScanner(f)
	lineNum := 0
	var descLines []string
	metadataSeen := false

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Skip shebang and filename lines
		if lineNum <= 2 {
			continue
		}

		// Stop at non-comment lines
		if !strings.HasPrefix(line, "#") {
			break
		}

		content := stripComment(line)

		// Stop at # --- separator
		if strings.HasPrefix(content, "---") {
			break
		}

		// Parse @key: value metadata lines; stop description collection here
		if strings.HasPrefix(content, "@") {
			parseMetadata(&info, content)
			metadataSeen = true
			continue
		}

		// Collect description lines (skip empty lines, stop after metadata)
		if content != "" && !metadataSeen {
			descLines = append(descLines, content)
		}
	}

	// Join description lines with space
	if len(descLines) > 0 {
		info.Description = strings.Join(descLines, " ")
	}

	return info, nil
}

// parseDetailedHeader reads the script header to extract description and usage.
// Parsing stops at # --- separator, aligning with Go tools' // --- pattern.
// Opens the file once, collecting both description and usage in a single pass.
func parseDetailedHeader(path string) (ScriptInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return ScriptInfo{}, err
	}
	defer f.Close()

	name := filepath.Base(path)
	info := ScriptInfo{
		Name:        name,
		Environment: classifyEnvironment(name),
		Category:    classifyCategory(name),
	}

	scanner := bufio.NewScanner(f)
	lineNum := 0
	var descLines []string
	var usageLines []string
	inUsage := false
	metadataSeen := false

	for scanner.Scan() {
		lineNum++
		if lineNum > 50 { // Only scan first 50 lines for header
			break
		}
		line := scanner.Text()

		// Skip shebang and filename lines
		if lineNum <= 2 {
			continue
		}

		// Stop at non-comment lines
		if !strings.HasPrefix(line, "#") {
			break
		}

		stripped := stripComment(line)

		// Stop at # --- separator (aligns with Go tools' // --- pattern)
		if strings.HasPrefix(stripped, "---") {
			break
		}

		// Parse @key: value metadata lines; stop description collection here
		if strings.HasPrefix(stripped, "@") {
			parseMetadata(&info, stripped)
			metadataSeen = true
			continue
		}

		// Detect usage section
		if strings.HasPrefix(strings.ToLower(stripped), "usage:") || strings.HasPrefix(stripped, "使用法:") {
			inUsage = true
			usageLines = append(usageLines, stripped)
			continue
		}

		if inUsage {
			// End of usage section: empty comment
			if stripped == "" {
				inUsage = false
				continue
			}
			usageLines = append(usageLines, stripped)
		} else {
			// Collect description lines (skip empty lines, stop after metadata)
			if stripped != "" && !metadataSeen {
				descLines = append(descLines, stripped)
			}
		}
	}

	if len(descLines) > 0 {
		info.Description = strings.Join(descLines, " ")
	}
	if len(usageLines) > 0 {
		info.Usage = strings.Join(usageLines, "\n")
	}

	return info, nil
}

// parseMetadata reads a @key: value metadata line and updates info fields.
func parseMetadata(info *ScriptInfo, line string) {
	if strings.HasPrefix(line, "@advertise:") {
		val := strings.TrimSpace(strings.TrimPrefix(line, "@advertise:"))
		info.Advertise = val == "true"
	}
	if strings.HasPrefix(line, "@hidden:") {
		val := strings.TrimSpace(strings.TrimPrefix(line, "@hidden:"))
		info.Hidden = val == "true"
	}
	if strings.HasPrefix(line, "@env:") {
		val := strings.TrimSpace(strings.TrimPrefix(line, "@env:"))
		if val == "container" || val == "any" {
			info.Environment = val
		}
	}
	if strings.HasPrefix(line, "@category:") {
		val := strings.TrimSpace(strings.TrimPrefix(line, "@category:"))
		if val == "test" || val == "utility" {
			info.Category = val
		}
	}
}

func stripComment(line string) string {
	if strings.HasPrefix(line, "#") {
		return strings.TrimSpace(strings.TrimPrefix(line, "#"))
	}
	return strings.TrimSpace(line)
}

// classifyEnvironment returns the default Environment classification for a
// script name. There is no hardcoded filename list — every script defaults
// to "any" and opts into "container" explicitly via the @env: header tag
// (see parseMetadata), so container-only status travels with the script's
// own header instead of living in sandbox-mcp's source.
func classifyEnvironment(name string) string {
	return "any"
}

func classifyCategory(name string) string {
	if strings.HasPrefix(name, "test-") {
		return "test"
	}
	return "utility"
}
