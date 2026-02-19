package specmd

import (
	"testing"

	"github.com/codalotl/codalotl/internal/gocodetesting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConformanceDiffType(t *testing.T) {
	dd := gocodetesting.Dedent

	type tc struct {
		name     string
		spec     string
		impl     string
		wantOK   bool
		wantType DiffType
	}

	cases := []tc{
		{
			name: "example_exact_match",
			spec: dd(`
				// Foo does x.
				func Foo(b int) error
			`),
			impl: dd(`
				// Foo does x.
				func Foo(b int) error { return nil }
			`),
			wantOK: true,
		},
		{
			name: "example_no_comment_allows_impl_comment",
			spec: dd(`
				func Foo(b int) error
			`),
			impl: dd(`
				// Foo does x.
				func Foo(b int) error { return nil }
			`),
			wantOK: true,
		},
		{
			name: "example_added_field_is_ok",
			spec: dd(`
				type Foo struct {
					Foo int
				}
			`),
			impl: dd(`
				type Foo struct {
					Foo    int
					hidden int
				}
			`),
			wantOK: true,
		},
		{
			name: "example_added_const_is_ok",
			spec: dd(`
				const (
					LangRuby string = "ruby"
					LangGo   string = "go"
				)
			`),
			impl: dd(`
				const (
					LangRuby string = "ruby"
					LangGo   string = "go"
					LangRust string = "rust"
				)
			`),
			wantOK: true,
		},
		{
			name: "func_body_ignored",
			spec: dd(`
				func Foo() int
			`),
			impl: dd(`
				func Foo() int { return 1 }
			`),
			wantOK: true,
		},
		{
			name: "func_signature_mismatch_is_code_mismatch",
			spec: dd(`
				func Foo(a int) error
			`),
			impl: dd(`
				func Foo(a string) error { return nil }
			`),
			wantOK:   false,
			wantType: DiffTypeCodeMismatch,
		},
		{
			name: "decl_doc_comment_required_exact",
			spec: dd(`
				// Foo does x.
				func Foo() error
			`),
			impl: dd(`
				// Foo does y.
				func Foo() error { return nil }
			`),
			wantOK:   false,
			wantType: DiffTypeDocMismatch,
		},
		{
			name: "decl_doc_comment_whitespace_only_diff",
			spec: dd(`
				// DocWS does things.
				func DocWS()
			`),
			impl: dd(`
				// DocWS	does things.
				func DocWS() {}
			`),
			wantOK:   false,
			wantType: DiffTypeDocWhitespace,
		},
		{
			name: "decl_doc_block_comment_allowed_for_bodyless_func",
			spec: dd(`
				/*
				Foo does x.
				*/
				func Foo(b int) error
			`),
			impl: dd(`
				/*
				Foo does x.
				*/
				func Foo(b int) error { return nil }
			`),
			wantOK: true,
		},
		{
			name: "spec_missing_comment_allows_impl_comment",
			spec: dd(`
				func Foo()
			`),
			impl: dd(`
				// extra comment ok
				func Foo() {}
			`),
			wantOK: true,
		},
		{
			name: "field_comment_required_exact",
			spec: dd(`
				type T struct {
					A int // a
				}
			`),
			impl: dd(`
				type T struct {
					A int // b
				}
			`),
			wantOK:   false,
			wantType: DiffTypeDocMismatch,
		},
		{
			name: "field_comment_spot_mismatch_doc_vs_eol",
			spec: dd(`
				type T struct {
					A int // a
				}
			`),
			impl: dd(`
				type T struct {
					// a
					A int
				}
			`),
			wantOK:   false,
			wantType: DiffTypeDocMismatch,
		},
		{
			name: "interface_extra_method_is_ok",
			spec: dd(`
				type I interface {
					A()
				}
			`),
			impl: dd(`
				type I interface {
					A()
					B()
				}
			`),
			wantOK: true,
		},
		{
			name: "interface_method_signature_mismatch_is_code_mismatch",
			spec: dd(`
				type I interface {
					A(a int) error
				}
			`),
			impl: dd(`
				type I interface {
					A(a string) error
				}
			`),
			wantOK:   false,
			wantType: DiffTypeCodeMismatch,
		},
		{
			name: "var_block_extra_element_is_ok",
			spec: dd(`
				var (
					A int
				)
			`),
			impl: dd(`
				var (
					A int
					B int
				)
			`),
			wantOK: true,
		},
		{
			name: "type_block_extra_element_is_ok",
			spec: dd(`
				type (
					A int
				)
			`),
			impl: dd(`
				type (
					A int
					B int
				)
			`),
			wantOK: true,
		},
		{
			name: "struct_missing_required_field_is_code_mismatch",
			spec: dd(`
				type T struct {
					A int
				}
			`),
			impl: dd(`
				type T struct{}
			`),
			wantOK:   false,
			wantType: DiffTypeCodeMismatch,
		},
		{
			name: "value_spec_extra_name_with_parallel_values_is_ok",
			spec: dd(`
				const (
					A = 1
				)
			`),
			impl: dd(`
				const (
					A, B = 1, 2
				)
			`),
			wantOK: true,
		},
		{
			name: "nested_struct_extra_field_is_ok",
			spec: dd(`
				type T struct {
					N struct {
						A int
					}
				}
			`),
			impl: dd(`
				type T struct {
					N struct {
						A int
						B int
					}
				}
			`),
			wantOK: true,
		},
		{
			name: "nested_struct_map_key_extra_field_is_ok",
			spec: dd(`
				type T struct {
					M map[struct {
						A int
					}]int
				}
			`),
			impl: dd(`
				type T struct {
					M map[struct {
						A int
						B int
					}]int
				}
			`),
			wantOK: true,
		},
		{
			name: "nested_struct_map_key_missing_required_field_is_code_mismatch",
			spec: dd(`
				type T struct {
					M map[struct {
						A int
					}]int
				}
			`),
			impl: dd(`
				type T struct {
					M map[struct{}]int
				}
			`),
			wantOK:   false,
			wantType: DiffTypeCodeMismatch,
		},
		{
			name: "nested_struct_map_key_field_comment_required_exact",
			spec: dd(`
				type T struct {
					M map[struct {
						A int // a
					}]int
				}
			`),
			impl: dd(`
				type T struct {
					M map[struct {
						A int // b
						B int
					}]int
				}
			`),
			wantOK:   false,
			wantType: DiffTypeDocMismatch,
		},
		{
			name: "nested_struct_map_key_field_comment_whitespace_only_diff",
			spec: dd(`
				type T struct {
					M map[struct {
						A int // a b
					}]int
				}
			`),
			impl: dd(`
				type T struct {
					M map[struct {
						A int // a	b
						B int
					}]int
				}
			`),
			wantOK:   false,
			wantType: DiffTypeDocWhitespace,
		},
		{
			name: "nested_struct_deep_extra_field_is_ok",
			spec: dd(`
				type T struct {
					N struct {
						Inner struct {
							A int
						}
					}
				}
			`),
			impl: dd(`
				type T struct {
					N struct {
						Inner struct {
							A int
							B int
						}
					}
				}
			`),
			wantOK: true,
		},
		{
			name: "nested_struct_missing_required_field_is_code_mismatch",
			spec: dd(`
				type T struct {
					N struct {
						A int
					}
				}
			`),
			impl: dd(`
				type T struct {
					N struct{}
				}
			`),
			wantOK:   false,
			wantType: DiffTypeCodeMismatch,
		},
		{
			name: "nested_struct_field_comment_required_exact",
			spec: dd(`
				type T struct {
					N struct {
						A int // a
					}
				}
			`),
			impl: dd(`
				type T struct {
					N struct {
						A int // b
						B int
					}
				}
			`),
			wantOK:   false,
			wantType: DiffTypeDocMismatch,
		},
		{
			name: "interface_method_returning_struct_literal_is_not_subject_to_recursive_field_rules",
			spec: dd(`
				type I interface {
					A() struct {
						X int
					}
				}
			`),
			impl: dd(`
				type I interface {
					A() struct {
						X int
						Y int
					}
				}
			`),
			wantOK:   false,
			wantType: DiffTypeCodeMismatch,
		},
		{
			name: "struct_field_func_returning_struct_literal_is_not_subject_to_recursive_field_rules",
			spec: dd(`
				type T struct {
					F func() struct {
						X int
					}
				}
			`),
			impl: dd(`
				type T struct {
					F func() struct {
						X int
						Y int
					}
				}
			`),
			wantOK:   false,
			wantType: DiffTypeCodeMismatch,
		},
		{
			name: "code_mismatch_takes_precedence_over_doc_mismatch",
			spec: dd(`
				// Foo does x.
				func Foo(a int)
			`),
			impl: dd(`
				// Foo does y.
				func Foo(a string) {}
			`),
			wantOK:   false,
			wantType: DiffTypeCodeMismatch,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ok, dt, err := conformanceDiffType(c.spec, c.impl)
			require.NoError(t, err)
			assert.Equal(t, c.wantOK, ok)
			if !c.wantOK {
				assert.Equal(t, c.wantType, dt)
			}
		})
	}
}
