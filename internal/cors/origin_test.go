package cors

import "testing"

func TestParse_Valid(t *testing.T) {
	cases := []struct {
		in       string
		wildcard bool
	}{
		{"http://example.com", false},
		{"https://example.com", false},
		{"https://api.example.com:8443", false},
		{"https://*.example.com", true},
		{"http://*.example.com", true},
	}
	for _, c := range cases {
		p, err := Parse(c.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.in, err)
			continue
		}
		if p.wildcard != c.wildcard {
			t.Errorf("Parse(%q).wildcard = %v, want %v", c.in, p.wildcard, c.wildcard)
		}
	}
}

func TestParse_Invalid(t *testing.T) {
	cases := []string{
		"",
		"*",
		"example.com",
		"ftp://example.com",
		"https://",
		"https://user@example.com",
		"https://**.example.com",
		"https://example.*.com",
		"https://*.com",
		"https://*",
		"https://example.*",
	}
	for _, in := range cases {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q): expected error, got nil", in)
		}
	}
}

func TestMatch(t *testing.T) {
	cases := []struct {
		pattern string
		origin  string
		want    bool
	}{
		// literal pattern
		{"https://example.com", "https://example.com", true},
		{"https://example.com", "http://example.com", false},
		{"https://example.com", "https://api.example.com", false},
		{"https://example.com", "https://example.com.evil.com", false},
		{"https://example.com", "", false},

		// wildcard pattern
		{"https://*.example.com", "https://api.example.com", true},
		{"https://*.example.com", "https://a.b.example.com", false},
		{"https://*.example.com", "https://example.com", false},
		{"https://*.example.com", "https://*.example.com", false},
		{"https://*.example.com", "http://api.example.com", false},
		{"https://*.example.com", "https://api.example.org", false},
	}
	for _, c := range cases {
		p, err := Parse(c.pattern)
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.pattern, err)
		}
		if got := p.Match(c.origin); got != c.want {
			t.Errorf("Match(%q, %q) = %v, want %v", c.pattern, c.origin, got, c.want)
		}
	}
}
