package clarifyintegration

// Greeter formats public greetings.
type Greeter struct{}

// Hello returns a greeting for name in the format "hello, <name>".
func (Greeter) Hello(name string) string {
	return "hello, " + name
}
