package mypkg_test

import (
	"mypkg"
	"testing"
	"time"
)

func TestReading(t *testing.T) {
	r := mypkg.NewReading(Temperature(10), "")
	if r.Value != Temperature(10) {
		t.Errorf("wrong temp (not 10)")
	}
	assertAboutNow(t, r)
}

func assertAboutNow(t *testing.T, r mypkg.Reading) {
	t.Helper()
	if time.Since(r.Timestamp) > 100*time.Millisecond {
		t.Errorf("reading timestamp is stale")
	}
}
