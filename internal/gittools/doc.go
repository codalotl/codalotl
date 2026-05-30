// Package gittools provides helpers for finding the git base and changed paths for the current line of work.
//
// Use HeuristicMergeBase to choose a best-effort base commit and optional ref, then use ChangedPathsSince to list repo-relative paths changed since that base. The
// package shells out to git and may use gh, when available, as an optional hint for GitHub pull requests.
package gittools
