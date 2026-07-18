package frame

import "testing"

func TestSanitize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain text", "plain text"},
		{"soft\u00adhyphen", "softhyphen"},
		{"zero\u200bwidth\u200e", "zerowidth"},
		{"tab\there", "tab here"},
		{"cr\r\nkept", "cr\nkept"},
		{"\x1b[31mred\x1b[0m", "\x1b[31mred\x1b[0m"}, // SGR survives
		{"a\ufeffb\u2028c", "abc"},
	}
	for _, c := range cases {
		if got := Sanitize(c.in); got != c.want {
			t.Errorf("Sanitize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
