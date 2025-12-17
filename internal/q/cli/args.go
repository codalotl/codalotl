package cli

import "fmt"

// NoArgs validates that there are no positional args.
func NoArgs(args []string) error {
	if len(args) == 0 {
		return nil
	}
	return usageErrorf("expected no args, got %d", len(args))
}

// ExactArgs returns an ArgsFunc that validates that exactly n args are provided.
func ExactArgs(n int) ArgsFunc {
	return func(args []string) error {
		if len(args) == n {
			return nil
		}
		return usageErrorf("expected %s, got %d", pluralArgs(n), len(args))
	}
}

// MinimumArgs returns an ArgsFunc that validates that at least n args are provided.
func MinimumArgs(n int) ArgsFunc {
	return func(args []string) error {
		if len(args) >= n {
			return nil
		}
		return usageErrorf("expected at least %s, got %d", pluralArgs(n), len(args))
	}
}

// RangeArgs returns an ArgsFunc that validates that between min and max args are provided (inclusive).
func RangeArgs(min, max int) ArgsFunc {
	return func(args []string) error {
		if len(args) >= min && len(args) <= max {
			return nil
		}
		return usageErrorf("expected %s-%s, got %d", pluralArgs(min), pluralArgs(max), len(args))
	}
}

func pluralArgs(n int) string {
	if n == 1 {
		return "1 arg"
	}
	return fmt.Sprintf("%d args", n)
}

