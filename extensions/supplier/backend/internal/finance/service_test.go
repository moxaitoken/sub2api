package finance

import "testing"

func TestParseAmountExact(t *testing.T) {
	for value, want := range map[string]int64{"0.01": 1, "1": 100, "10.1": 1010, "92233720368547758.07": 9223372036854775807} {
		got, e := ParseAmount(value)
		if e != nil || got != want {
			t.Fatalf("%q=%d %v", value, got, e)
		}
	}
	for _, value := range []string{"0", "0.00", "-1", "1.001", "1e3", " 1", "01", "1.", "NaN", "92233720368547758.08"} {
		if _, e := ParseAmount(value); e == nil {
			t.Fatalf("accepted %q", value)
		}
	}
}
