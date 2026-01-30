package mypkg

import "time"

type Reading struct {
	Value     Temperature
	Timestamp time.Time
	location  string
}

var DefaultLocation = "Unknown"

func NewReading(t Temperature, location string) Reading {
	if location == "" {
		location = DefaultLocation
	}
	return Reading{
		Value:     t,
		Timestamp: time.Now(),
		location:  location,
	}
}
