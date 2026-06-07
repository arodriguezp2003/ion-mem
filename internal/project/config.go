package project

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// configFile is the structure of .ion-mem/config.json.
// The JSON key is "project" (locked decision #1 — intentional divergence from
// engram which uses "project_name").
type configFile struct {
	Project string `json:"project"`
}

// readConfig walks upward from cwd to repoRoot looking for the nearest
// .ion-mem/config.json. Returns (cfg, dir, true, nil) when found and valid,
// (zero, "", false, nil) when not found or project field is empty after trim,
// or (zero, "", false, err) on I/O or JSON parse errors.
//
// The returned dir is the directory that contained the nearest valid config.
//
// Walk-up behavior (locked decision #7, engram parity): nearest config wins;
// walk stops at repoRoot (does not cross the git repo boundary). When
// cwd == repoRoot, only that directory is checked.
//
// Malformed JSON returns error; DetectFull treats this as fall-through
// (spec R-ALGO-03).
func readConfig(cwd, repoRoot string) (cfg configFile, dir string, found bool, err error) {
	cwd = filepath.Clean(cwd)
	repoRoot = filepath.Clean(repoRoot)

	current := cwd
	for {
		configPath := filepath.Join(current, ".ion-mem", "config.json")
		data, readErr := os.ReadFile(configPath)
		if readErr != nil {
			if !errors.Is(readErr, os.ErrNotExist) {
				// Unexpected I/O error — propagate.
				return configFile{}, "", false, fmt.Errorf("project: readconfig: %w", readErr)
			}
			// File not found — continue walking up.
		} else {
			// File found — try to parse it.
			var c configFile
			if jsonErr := json.Unmarshal(data, &c); jsonErr != nil {
				// Malformed JSON returns error; DetectFull treats this as
				// fall-through (spec R-ALGO-03).
				return configFile{}, "", false, fmt.Errorf("project: parseconfig: %w", jsonErr)
			}
			// Empty project field after trim → continue walking (locked decision #8).
			if strings.TrimSpace(c.Project) == "" {
				// Do NOT return an error; just skip this config and keep walking.
				// Fall through to boundary check below.
			} else {
				return c, current, true, nil
			}
		}

		// Stop when we've checked repoRoot.
		if current == repoRoot {
			break
		}

		parent := filepath.Dir(current)
		if parent == current {
			// Reached filesystem root — stop.
			break
		}
		current = parent
	}

	return configFile{}, "", false, nil
}
