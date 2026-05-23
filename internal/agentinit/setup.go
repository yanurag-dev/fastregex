// Package agentinit wires GrepTurbo instructions into the user's global Claude Code config.
package agentinit

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed GrepTurbo.md
var assets embed.FS

const (
	claudeDir      = ".claude"
	claudeMDFile   = "CLAUDE.md"
	grepturboMD    = "GrepTurbo.md"
	includeDirective = "@GrepTurbo.md"
)

// Setup writes GrepTurbo.md to ~/.claude/ and ensures ~/.claude/CLAUDE.md includes it.
// Returns a human-readable summary of what changed.
func Setup() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home dir: %w", err)
	}

	claudePath := filepath.Join(home, claudeDir)
	if err := os.MkdirAll(claudePath, 0o755); err != nil {
		return "", fmt.Errorf("cannot create %s: %w", claudePath, err)
	}

	// Write GrepTurbo.md from embedded asset
	content, err := assets.ReadFile(grepturboMD)
	if err != nil {
		return "", fmt.Errorf("embedded asset missing: %w", err)
	}
	mdDest := filepath.Join(claudePath, grepturboMD)
	if err := os.WriteFile(mdDest, content, 0o644); err != nil {
		return "", fmt.Errorf("cannot write %s: %w", mdDest, err)
	}

	// Patch CLAUDE.md
	claudeMDPath := filepath.Join(claudePath, claudeMDFile)
	var msg string
	existing, err := os.ReadFile(claudeMDPath)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("cannot read %s: %w", claudeMDPath, err)
	}

	if strings.Contains(string(existing), includeDirective) {
		msg = fmt.Sprintf("already configured — %s already references GrepTurbo.md", claudeMDPath)
	} else {
		var newContent string
		if len(existing) == 0 {
			newContent = includeDirective + "\n"
		} else {
			newContent = string(existing)
			if !strings.HasSuffix(newContent, "\n") {
				newContent += "\n"
			}
			newContent += "\n" + includeDirective + "\n"
		}
		if err := os.WriteFile(claudeMDPath, []byte(newContent), 0o644); err != nil {
			return "", fmt.Errorf("cannot write %s: %w", claudeMDPath, err)
		}
		msg = fmt.Sprintf("added %s to %s", includeDirective, claudeMDPath)
	}

	return fmt.Sprintf("wrote %s\n%s", mdDest, msg), nil
}
