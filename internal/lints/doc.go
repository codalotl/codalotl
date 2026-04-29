// Package lints configures and runs lint pipelines for Go package directories.
//
// It resolves user configuration into ordered steps, selects check or fix behavior for each situation, and reports command results as cmdrunner lint-status XML.
package lints