package mypkg

import "testing"

func TestAverage(t *testing.T) {
	avg := Average(tempsFreezingBoiling())
	if avg != Temperature(50) {
		t.Errorf("avg not 50")
	}
}

func tempsFreezingBoiling() []Temperature {
	return []Temperature{Freezing, Boiling}
}
