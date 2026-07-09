package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTfToolFromDir(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"opentofu-version", map[string]string{".opentofu-version": "1.8.0"}, "tofu"},
		{"terraform-version", map[string]string{".terraform-version": "1.5.7"}, "terraform"},
		{"tfswitchrc", map[string]string{".tfswitchrc": "1.5.7"}, "terraform"},
		{"tool-versions terraform", map[string]string{".tool-versions": "nodejs 20.0.0\nterraform 1.5.7\n"}, "terraform"},
		{"tool-versions opentofu", map[string]string{".tool-versions": "opentofu 1.8.0\n"}, "tofu"},
		{"none", map[string]string{"main.tf": "# nothing here"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, body := range c.files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if got := tfToolFromDir(dir); got != c.want {
				t.Errorf("tfToolFromDir = %q, want %q", got, c.want)
			}
		})
	}
}

func TestPreferredTfToolWalksParents(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".opentofu-version"), []byte("1.8.0"), 0o644); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(root, "envs", "prod")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := preferredTfTool(child); got != "tofu" {
		t.Errorf("preferredTfTool(child) = %q, want tofu (found in parent)", got)
	}
}
