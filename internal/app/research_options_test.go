package app

import (
	"io"
	"testing"
)

func TestResearchOptionsAreRepeatableAndBounded(t *testing.T) {
	o, err := Parse([]string{"--research=first.csv", "--research", "second.worldr-data.json"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Research) != 2 || o.Research[0] != "first.csv" || o.Research[1] != "second.worldr-data.json" {
		t.Fatal("research paths lost order", o.Research)
	}
	flags := make([]string, 9)
	for i := range flags {
		flags[i] = "--research=data.csv"
	}
	if _, err := Parse(flags, io.Discard); err == nil {
		t.Fatal("accepted more than eight research dashboards")
	}
	if _, err := Parse([]string{"--research="}, io.Discard); err == nil {
		t.Fatal("accepted an empty research path")
	}
}
