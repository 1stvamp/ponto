package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestPayloadScript(t *testing.T) {
	s, err := payloadScript("graph", map[string]int{"a": 1})
	if err != nil {
		t.Fatal(err)
	}
	if s != `<script>window.graph = {"a":1}</script>` {
		t.Errorf("unexpected script: %q", s)
	}
}

func TestPayloadScriptEscapesHTML(t *testing.T) {
	s, err := payloadScript("rso", "</script><b>")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s, "</script><b>") {
		t.Errorf("payload not HTML-escaped, could break out of the script tag: %q", s)
	}
	if !strings.Contains(s, `<`) {
		t.Errorf("expected escaped < (\\u003c) in %q", s)
	}
}

func TestGenerateStaticHTMLInlinesPayloads(t *testing.T) {
	fe := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte("<html><head><title>x</title></head><body><div id=app></div></body></html>"),
		},
	}
	r := &ponto{}
	out := filepath.Join(t.TempDir(), "ponto.html")
	if err := r.generateStaticHTML(fe, out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	html := string(b)
	for _, want := range []string{"window.graph", "window.map", "window.rso"} {
		if !strings.Contains(html, want) {
			t.Errorf("output missing %q", want)
		}
	}
	if strings.Index(html, "window.graph") > strings.Index(html, "</head>") {
		t.Error("payload scripts must be injected before </head>")
	}
}

func TestGenerateStaticHTMLErrorsWithoutHead(t *testing.T) {
	fe := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html><body></body></html>")},
	}
	r := &ponto{}
	err := r.generateStaticHTML(fe, filepath.Join(t.TempDir(), "x.html"))
	if err == nil {
		t.Fatal("expected an error when index.html has no </head>")
	}
}
