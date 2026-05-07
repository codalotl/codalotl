// Package refactor provides an LLM tool for running registered, package-local Go refactors.
//
// Use NewRefactorTool to construct the tool. Tool calls name a registered refactor and a target package; results report whether the refactor was applied and which
// package-relative files changed.
package refactor
