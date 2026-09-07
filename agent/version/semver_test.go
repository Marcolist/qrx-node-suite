package version

import "testing"

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"0.0.7", "0.0.6", 1},
		{"1.0.0", "1.0.0-rc1", 1},
		{"1.0.0-rc1", "1.0.0", -1},
		{"2.0.0", "1.9.9", 1},
	}
	for _, c := range cases {
		got, err := CompareStrict(c.a, c.b)
		if err != nil {
			t.Fatalf("CompareStrict(%q,%q) error: %v", c.a, c.b, err)
		}
		if got != c.want {
			t.Errorf("CompareStrict(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestSatisfies(t *testing.T) {
	cases := []struct {
		v, r string
		want bool
	}{
		{"0.2.0", ">=0.1.0", true},
		{"0.0.9", ">=0.1.0", false},
		{"1.2.3", "^1.0.0", true},
		{"2.0.0", "^1.0.0", false},
		{"1.2.3", "~1.2.0", true},
		{"1.3.0", "~1.2.0", false},
		{"0.0.7", "0.0.7", true},
		{"0.0.8", "0.0.7", false},
		{"9.9.9", "*", true},
	}
	for _, c := range cases {
		got := Satisfies(c.v, c.r)
		if got != c.want {
			t.Errorf("Satisfies(%q,%q) = %v, want %v", c.v, c.r, got, c.want)
		}
	}
}

func TestInvalidVersionRejected(t *testing.T) {
	if IsValidSemVer("not-a-version") {
		t.Error("expected invalid version to be rejected")
	}
	if IsValidSemVer("1.2") {
		t.Error("expected two-component version to be rejected")
	}
}
