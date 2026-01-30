package uses

import "github.com/codalotl/codalotl/internal/gotypes/testdata/lib"

type Wrapper struct {
	foo lib.Foo
}

func NewWrapper() Wrapper {
	return Wrapper{foo: lib.NewFoo()}
}

func (w Wrapper) Value() int {
	return w.foo.Value
}
