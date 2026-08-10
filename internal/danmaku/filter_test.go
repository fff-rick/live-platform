package danmaku

import "testing"

func TestSensitiveFilter(t *testing.T) {
	f := NewSensitiveFilter([]string{"spam", "坏词"})
	for _, tc := range []struct {
		text string
		want bool
	}{
		{"hello", false}, {"this is SPAM text", true}, {"包含坏词", true},
	} {
		if got := f.Contains(tc.text); got != tc.want {
			t.Fatalf("Contains(%q)=%v want %v", tc.text, got, tc.want)
		}
	}
}
