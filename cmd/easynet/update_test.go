package main

import "testing"

func TestVersionLess(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.3.0", "0.3.1", true},
		{"0.3.1", "0.3.0", false},
		{"0.3.0", "0.3.0", false},
		{"v0.2.1", "0.3.0", true},
		{"0.10.0", "0.9.1", false},
		{"1.0.0", "0.9.9", false},
		{"0.4.0-rc1", "0.4.0", false},
	}
	for _, c := range cases {
		if got := versionLess(c.a, c.b); got != c.want {
			t.Errorf("versionLess(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
