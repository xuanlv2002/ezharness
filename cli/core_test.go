package main

import "testing"

func TestNormalizeServer(t *testing.T) {
	cases := []struct {
		in   string
		want string
		bad  bool
	}{
		{"localhost:5260", "http://localhost:5260", false},
		{"127.0.0.1", "http://127.0.0.1:5260", false},
		{"192.168.1.5:8080", "http://192.168.1.5:8080", false},
		{"http://example.com", "http://example.com", false},
		{"https://example.com/foo", "https://example.com", false},
		{"ftp://example.com", "", true},
		{"", "", true},
	}
	for _, c := range cases {
		got, err := normalizeServer(c.in)
		if c.bad {
			if err == nil {
				t.Fatalf("normalizeServer(%q) should fail, got %q", c.in, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Fatalf("normalizeServer(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
}
