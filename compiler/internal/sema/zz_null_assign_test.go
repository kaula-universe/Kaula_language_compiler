package sema

import "testing"

func TestNullAssignable(t *testing.T) {
	cases := []struct{ e, a string; want bool }{
		{"void*", "null", true},
		{"i64*", "null", true},
		{"string?", "null", true},
		{"void(void())void()", "null", true},
		{"i64", "null", false},
		{"string", "null", false},
	}
	for _, c := range cases {
		got := typesAssignable(c.e, c.a)
		if got != c.want {
			t.Errorf("typesAssignable(%q,%q)=%v want %v", c.e, c.a, got, c.want)
		}
	}
}
