package mypkg

type Temperature int

const (
	Freezing Temperature = 0
	Boiling  Temperature = 100
)

// Celsius returns the temperature in °C as an int.
func (t Temperature) Celsius() int {
	return int(t)
}

func (t Temperature) AboveFreezing() bool {
	return t.above(Freezing)
}

func (t Temperature) above(threshold Temperature) bool {
	return t > threshold
}
