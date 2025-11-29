package gocode

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractPackageName(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		want    string
		wantErr bool
	}{
		{
			name: "basic main",
			src:  "package main\n",
			want: "main",
		},
		{
			name: "package with trailing semicolon and comment",
			src:  "package foo; // the best pkg\n",
			want: "foo",
		},
		{
			name: "UTF‑8 BOM then package",
			src:  "\uFEFFpackage bom\n",
			want: "bom",
		},
		{
			name: "build tags then package",
			src: `//go:build !windows
// +build !windows

package unix
`,
			want: "unix",
		},
		{
			name: "block comment before package",
			src: `/* license
multi‑line */
package commented
`,
			want: "commented",
		},
		{
			name: "extra spaces and tabs",
			src:  "\n\t package    spaced \t\n",
			want: "spaced",
		},
		{
			name:    "no package declaration",
			src:     "// just a comment\nvar x int\n",
			wantErr: true,
		},
		{
			name:    "code before package",
			src:     "var y int\npackage later\n",
			wantErr: true,
		},
		{
			name:    "invalid identifier",
			src:     "package 123abc\n",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		tc := tc // capture
		t.Run(tc.name, func(t *testing.T) {
			got, err := extractPackageName([]byte(tc.src))
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.want, got)
			}
		})
	}
}
