package gocode

import (
	"go/build"
	"os"
	"strings"
)

// goFilesInDirForConfig returns the .go files in dir for the default build, as modified by goarch, goos, and buildTags. If goarch, goos, or buildTags is the zero
// value, it is ignored and does not modify the default build. In addition, it reads GOFLAGS and appends that to buildTags.
func goFilesInDirForConfig(dir string, goarch string, goos string, buildTags []string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	ctx := build.Default
	if goarch != "" {
		ctx.GOARCH = goarch
	}
	if goos != "" {
		ctx.GOOS = goos
	}

	// Merge explicit buildTags with any -tags found in GOFLAGS.
	var mergedTags []string
	if len(buildTags) > 0 {
		mergedTags = append(mergedTags, buildTags...)
	}
	if gf := os.Getenv("GOFLAGS"); gf != "" {
		if tags := ParseTagsFromGOFLAGS(gf); len(tags) > 0 {
			mergedTags = append(mergedTags, tags...)
		}
	}
	if len(mergedTags) > 0 {
		// Deduplicate while preserving order
		seen := make(map[string]struct{}, len(mergedTags))
		unique := make([]string, 0, len(mergedTags))
		for _, t := range mergedTags {
			if t == "" {
				continue
			}
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			unique = append(unique, t)
		}
		ctx.BuildTags = unique
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasPrefix(name, ".") {
			continue
		}

		match, err := ctx.MatchFile(dir, name)
		if err != nil {
			return nil, err
		}
		if match {
			files = append(files, name)
		}
	}

	return files, nil
}

// ParseTagsFromGOFLAGS parses GOFLAGS and extracts all values passed to -tags. It supports both "-tags=foo,bar" and "-tags foo,bar" forms. It uses strings.Fields
// for tokenization and does not perform full shell parsing.
func ParseTagsFromGOFLAGS(gf string) []string {
	if gf == "" {
		return nil
	}
	var out []string
	parts := strings.Fields(gf)
	for i := 0; i < len(parts); i++ {
		p := parts[i]
		if strings.HasPrefix(p, "-tags=") {
			val := strings.TrimPrefix(p, "-tags=")
			for _, t := range strings.Split(val, ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					out = append(out, t)
				}
			}
			continue
		}
		if p == "-tags" && i+1 < len(parts) {
			val := parts[i+1]
			i++
			for _, t := range strings.Split(val, ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					out = append(out, t)
				}
			}
		}
	}
	return out
}
