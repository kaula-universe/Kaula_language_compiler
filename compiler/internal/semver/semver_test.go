package semver

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		maj  int
		min  int
		pat  int
		want bool
	}{
		{"1.2.3", 1, 2, 3, true},
		{"v2.0.1", 2, 0, 1, true},
		{"0.12.0", 0, 12, 0, true},
		{"1.2", 1, 2, 0, true},
		{"1", 1, 0, 0, true},
		{"1.2.3-beta.1", 1, 2, 3, true},
		{"abc", 0, 0, 0, false},
		{"", 0, 0, 0, false},
	}
	for _, c := range cases {
		v, err := Parse(c.in)
		if c.want && err != nil {
			t.Errorf("Parse(%q) unexpected error: %v", c.in, err)
			continue
		}
		if !c.want {
			if err == nil {
				t.Errorf("Parse(%q) expected error, got %+v", c.in, v)
			}
			continue
		}
		if v.Major != c.maj || v.Minor != c.min || v.Patch != c.pat {
			t.Errorf("Parse(%q) = %d.%d.%d, want %d.%d.%d", c.in, v.Major, v.Minor, v.Patch, c.maj, c.min, c.pat)
		}
	}
}

func TestConstraintMatches(t *testing.T) {
	cases := []struct {
		con  string
		ver  string
		want bool
	}{
		// 精确
		{"1.2.3", "1.2.3", true},
		{"1.2.3", "1.2.4", false},
		// 主.次 通配
		{"1.2", "1.2.9", true},
		{"1.2", "1.3.0", false},
		// 主版本
		{"1", "1.9.9", true},
		{"1", "2.0.0", false},
		// caret
		{"^1.2.3", "1.2.3", true},
		{"^1.2.3", "1.9.0", true},
		{"^1.2.3", "2.0.0", false},
		{"^0.2.3", "0.2.9", true},
		{"^0.2.3", "0.3.0", false},
		{"^0.0.3", "0.0.3", true},
		{"^0.0.3", "0.0.4", false},
		// tilde
		{"~1.2.3", "1.2.9", true},
		{"~1.2.3", "1.3.0", false},
		{"~1.2", "1.2.0", true},
		{"~1.2", "1.3.0", false},
		// wildcard
		{"1.x", "1.5.0", true},
		{"1.x", "2.0.0", false},
		{"1.2.x", "1.2.7", true},
		{"1.2.x", "1.3.0", false},
		// any
		{"*", "99.0.0", true},
		{"", "0.1.0", true},
	}
	for _, c := range cases {
		con, err := ParseConstraint(c.con)
		if err != nil {
			t.Errorf("ParseConstraint(%q) error: %v", c.con, err)
			continue
		}
		v, err := Parse(c.ver)
		if err != nil {
			t.Errorf("Parse(%q) error: %v", c.ver, err)
			continue
		}
		if got := con.Matches(v); got != c.want {
			t.Errorf("constraint %q matches %q = %v, want %v", c.con, c.ver, got, c.want)
		}
	}
}

func TestBestMatch(t *testing.T) {
	// ^1.2 => >=1.2.0 <2.0.0, 最高是 1.3.0
	con := MustParseConstraint(t, "^1.2")
	versions := []string{"1.0.0", "1.2.0", "1.2.5", "1.3.0", "2.0.0", "v1.2.9"}
	if got := BestMatch(con, versions); got != "1.3.0" {
		t.Errorf("BestMatch = %q, want 1.3.0", got)
	}

	con2 := MustParseConstraint(t, "~0.12")
	versions2 := []string{"0.12.0", "0.12.3", "0.13.0"}
	if got := BestMatch(con2, versions2); got != "0.12.3" {
		t.Errorf("BestMatch = %q, want 0.12.3", got)
	}

	con3 := MustParseConstraint(t, "2.0.0")
	if got := BestMatch(con3, []string{"1.0.0", "2.0.0", "2.0.1"}); got != "2.0.0" {
		t.Errorf("BestMatch = %q, want 2.0.0", got)
	}

	// 无匹配
	con4 := MustParseConstraint(t, "^3.0")
	if got := BestMatch(con4, []string{"1.0.0", "2.0.0"}); got != "" {
		t.Errorf("BestMatch = %q, want empty", got)
	}
}

func MustParseConstraint(t *testing.T, s string) Constraint {
	t.Helper()
	c, err := ParseConstraint(s)
	if err != nil {
		t.Fatalf("ParseConstraint(%q) error: %v", s, err)
	}
	return c
}
