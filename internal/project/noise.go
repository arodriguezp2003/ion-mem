package project

// noiseDirs is the set of directory names skipped during git_child scanning.
// These are common build artifact, dependency, and IDE directories that are
// never meaningful project roots. Filtering is case-sensitive (R-NOISE-03).
var noiseDirs = map[string]struct{}{
	"node_modules": {},
	"vendor":       {},
	".venv":        {},
	"venv":         {},
	"target":       {},
	"dist":         {},
	"build":        {},
	".idea":        {},
	".vscode":      {},
	".git":         {},
	"bin":          {},
	"out":          {},
	"cache":        {},
	"tmp":          {},
}
