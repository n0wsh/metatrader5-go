package webterm

import "testing"

func TestMatchGroup(t *testing.T) {
	cases := []struct {
		group  string
		symbol string
		want   bool
	}{
		{"*GOLD*", "GOLD", true},
		{"*GOLD*", "GOLDmicro", true},
		{"*GOLD*", "EURUSD", false},
		{"GOLD", "GOLD", true},
		{"GOLD", "GOLDmicro", false},
		{"GOLD*", "GOLDmicro", true},
		{"*USD", "EURUSD", true},
		{"*USD", "USD", true},
		{"*USD", "EURGBP", false},
		{"*,!*GOLD*", "GOLD", false},
		{"*,!*GOLD*", "EURUSD", true},
		{"*USD*,!EUR*", "GBPUSD", true},
		{"*USD*,!EUR*", "EURUSD", false},
		{"", "GOLD", false},
	}
	for _, c := range cases {
		if got := MatchGroup(c.group, c.symbol); got != c.want {
			t.Errorf("MatchGroup(%q, %q) = %v, want %v", c.group, c.symbol, got, c.want)
		}
	}
}
