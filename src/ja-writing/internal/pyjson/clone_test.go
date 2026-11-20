package pyjson

import "testing"

// Clone は鍵の並びと値の表を写す (写しに Set・Delete しても元は変わらない。値の list と dict は共有する)。
func TestCloneIsShallowCopy(t *testing.T) {
	inner := []Value{"a"}
	o := NewObject()
	o.Set("k", int64(1))
	o.Set("list", inner)
	c := o.Clone()
	c.Set("k", int64(2))
	c.Set("new", true)
	c.Delete("list")
	if v, _ := o.Get("k"); v != int64(1) {
		t.Errorf("写しへの Set が元を変えた: %v", v)
	}
	if o.Len() != 2 || len(o.Keys()) != 2 || o.Keys()[1] != "list" {
		t.Errorf("写しへの Set・Delete が元の鍵を変えた: %v", o.Keys())
	}
	if c.Len() != 2 || c.Keys()[0] != "k" || c.Keys()[1] != "new" {
		t.Errorf("写しの鍵が違う: %v", c.Keys())
	}
	shared, _ := o.Clone().Get("list")
	if &shared.([]Value)[0] != &inner[0] {
		t.Error("値の list を共有していない (浅い写しのはず)")
	}
}
