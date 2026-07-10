package main

import "testing"

func TestBrowserOpenArgs(t *testing.T) {
	cases := []struct {
		goos     string
		wantBin  string
		wantArg0 string
	}{
		{"linux", "xdg-open", "/tmp/ponto.html"},
		{"darwin", "open", "/tmp/ponto.html"},
		{"windows", "rundll32", "url.dll,FileProtocolHandler"},
	}
	for _, c := range cases {
		t.Run(c.goos, func(t *testing.T) {
			bin, args := browserOpenArgs(c.goos, "/tmp/ponto.html")
			if bin != c.wantBin {
				t.Errorf("bin = %q, want %q", bin, c.wantBin)
			}
			if len(args) == 0 || args[0] != c.wantArg0 {
				t.Errorf("args = %v, want first %q", args, c.wantArg0)
			}
		})
	}
}
