package brewledger

import "testing"

func TestParseIncludesOldnamesAliasesAndVersionless(t *testing.T) {
	set, err := Parse([]byte(`{"formulae":[
	  {"name":"postgresql@14","full_name":"postgresql@14","oldnames":["postgresql"],"aliases":["postgres"]},
	  {"name":"mysql@8.4","full_name":"homebrew/core/mysql@8.4","oldnames":[],"aliases":[]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"postgresql@14", "postgresql", "postgres", "mysql@8.4", "mysql"} {
		if !set[want] {
			t.Errorf("%q が台帳に無い", want)
		}
	}
	if set["redis"] {
		t.Error("入っていないものが入っている")
	}
	if _, err := Parse([]byte("{not json")); err == nil {
		t.Error("壊れた JSON を空集合にした (診断できずにすべき)")
	}
}
