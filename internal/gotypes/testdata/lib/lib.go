package lib

type Foo struct {
	Value int
}

func NewFoo() Foo {
	return Foo{Value: 42}
}
