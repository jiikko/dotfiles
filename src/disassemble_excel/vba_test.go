package main

import "testing"

func TestFindProcs(t *testing.T) {
	src := "Sub matrix()\nEnd Sub\n\nPrivate Function Calc(x As Long) As Long\nEnd Function\n\nSub gh用()\nEnd Sub\n\nPublic Property Get Name() As String\nEnd Property\n"

	procs := findProcs(src)

	want := []Proc{
		{Name: "matrix", Kind: "Sub", Line: 1},
		{Name: "Calc", Kind: "Function", Line: 4},
		{Name: "gh用", Kind: "Sub", Line: 7},
		{Name: "Name", Kind: "Property", Line: 10},
	}
	if len(procs) != len(want) {
		t.Fatalf("got %d procs, want %d: %+v", len(procs), len(want), procs)
	}
	for i, w := range want {
		if procs[i] != w {
			t.Errorf("proc[%d] = %+v, want %+v", i, procs[i], w)
		}
	}
}
