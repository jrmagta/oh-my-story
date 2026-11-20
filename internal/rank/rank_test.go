package rank

import (
	"math/rand/v2"
	"slices"
	"testing"
)

func TestKnownKeys(t *testing.T) {
	cases := []struct{ a, b, want string }{
		{"", "", "a0"},
		{"a0", "", "a1"},
		{"a1", "", "a2"},
		{"", "a0", "Zz"},
		{"az", "", "b00"},
		{"a0", "a1", "a0V"},
		{"a0V", "a1", "a0l"},
		{"Zz", "a0", "ZzV"},
		{"", "Zz", "Zy"},
		{"b00", "", "b01"},
	}
	for _, c := range cases {
		got, err := Between(c.a, c.b)
		if err != nil || got != c.want {
			t.Errorf("Between(%q, %q) = %q, %v; want %q", c.a, c.b, got, err, c.want)
		}
	}
}

func TestRejectsBadInput(t *testing.T) {
	for _, c := range [][2]string{{"a1", "a0"}, {"a0", "a0"}, {"a00", ""}, {"a", ""}, {"!", ""}, {"", smallestInteger}} {
		if _, err := Between(c[0], c[1]); err == nil {
			t.Errorf("Between(%q, %q) succeeded", c[0], c[1])
		}
	}
}

// Random inserts at random positions always produce a strictly ordered list,
// and appending many items keeps keys short.
func TestRandomInsertsStayOrdered(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	keys := []string{}
	for range 5000 {
		i := r.IntN(len(keys) + 1)
		a, b := "", ""
		if i > 0 {
			a = keys[i-1]
		}
		if i < len(keys) {
			b = keys[i]
		}
		k, err := Between(a, b)
		if err != nil {
			t.Fatalf("Between(%q, %q): %v", a, b, err)
		}
		if (a != "" && k <= a) || (b != "" && k >= b) {
			t.Fatalf("Between(%q, %q) = %q is out of order", a, b, k)
		}
		keys = slices.Insert(keys, i, k)
	}
	if !slices.IsSorted(keys) {
		t.Fatal("keys not sorted")
	}

	last := ""
	for range 10000 {
		k, err := Between(last, "")
		if err != nil {
			t.Fatal(err)
		}
		last = k
	}
	if len(last) > 4 {
		t.Errorf("10000 appends produced %q", last)
	}
	first := ""
	for range 10000 {
		k, err := Between("", first)
		if err != nil {
			t.Fatal(err)
		}
		first = k
	}
	if len(first) > 4 {
		t.Errorf("10000 prepends produced %q", first)
	}
}
