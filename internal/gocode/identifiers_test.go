package gocode

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDeparenthesizeIdentifier(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no receiver no change",
			in:   "DoThing",
			want: "DoThing",
		},
		{
			name: "already canonical pointer receiver",
			in:   "*SomeType.SomeMethod",
			want: "*SomeType.SomeMethod",
		},
		{
			name: "pointer receiver in parens",
			in:   "(*SomeType).SomeMethod",
			want: "*SomeType.SomeMethod",
		},
		{
			name: "non-pointer receiver in parens",
			in:   "(SomeType).SomeMethod",
			want: "SomeType.SomeMethod",
		},
		{
			name: "nested parens",
			in:   "((*SomeType)).SomeMethod",
			want: "*SomeType.SomeMethod",
		},
		{
			name: "package-qualified type in parens",
			in:   "(*pkg.SomeType).SomeMethod",
			want: "*pkg.SomeType.SomeMethod",
		},
		{
			name: "generic receiver pointer in parens",
			in:   "(*SomeType[T]).SomeMethod",
			want: "*SomeType.SomeMethod",
		},
		{
			name: "generic receiver non-pointer in parens",
			in:   "(SomeType[T]).SomeMethod",
			want: "SomeType.SomeMethod",
		},
		{
			name: "generic receiver multiple type params and pkg qualifier",
			in:   "(*pkg.SomeType[T, U]).SomeMethod",
			want: "*pkg.SomeType.SomeMethod",
		},
		{
			name: "generic receiver without parens",
			in:   "SomeType[T].SomeMethod",
			want: "SomeType.SomeMethod",
		},
		{
			name: "generic receiver without parens with pkg qualifier",
			in:   "pkg.SomeType[T, U].SomeMethod",
			want: "pkg.SomeType.SomeMethod",
		},
		{
			name: "invalid parse returns unchanged",
			in:   "(*).SomeMethod",
			want: "(*).SomeMethod",
		},
		{
			name: "unbalanced parens returns unchanged",
			in:   "(*SomeType.SomeMethod",
			want: "(*SomeType.SomeMethod",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, DeparenthesizeIdentifier(tc.in))
		})
	}
}
