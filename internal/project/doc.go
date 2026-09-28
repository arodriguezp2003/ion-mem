// Package project resolves a project name from a filesystem path.
// It applies a deterministic 5-case algorithm (config → git_remote → git_root →
// git_child → dir_basename) and always returns a usable result.
// The package is safe for concurrent use and never mutates the filesystem.
package project
