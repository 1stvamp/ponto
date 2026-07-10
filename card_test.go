package main

import (
	"bytes"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleModel() summaryModel {
	danger := tierTokens["danger"]
	caution := tierTokens["caution"]
	safe := tierTokens["safe"]
	return summaryModel{
		verdict:   "danger",
		counts:    summaryCounts{add: 1, change: 1, destroy: 1, replace: 1, total: 4},
		tierCount: map[string]int{"danger": 2, "caution": 1, "safe": 1},
		groups: []tierGroup{
			{tierToken: danger, resources: []resChange{
				{action: "destroy", glyph: "−", tier: "danger", short: "aws_s3_bucket.logs"},
				{action: "replace", glyph: "±", tier: "danger", short: "aws_instance.web"},
			}},
			{tierToken: caution, resources: []resChange{
				{action: "update", glyph: "~", tier: "caution", short: "aws_iam_role.app"},
			}},
			{tierToken: safe, resources: []resChange{
				{action: "create", glyph: "+", tier: "safe", short: "aws_sns_topic.alerts"},
			}},
		},
		resources: []resChange{
			{action: "destroy", glyph: "−", tier: "danger", short: "aws_s3_bucket.logs"},
			{action: "replace", glyph: "±", tier: "danger", short: "aws_instance.web"},
			{action: "update", glyph: "~", tier: "caution", short: "aws_iam_role.app"},
			{action: "create", glyph: "+", tier: "safe", short: "aws_sns_topic.alerts"},
		},
	}
}

func TestRenderCardPNG(t *testing.T) {
	for _, set := range []string{"dots", "signs", "none"} {
		t.Run(set, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "card.png")
			if err := renderCard(sampleModel(), set, "png", out); err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")) {
				t.Errorf("output is not a PNG (bad magic)")
			}
			if len(b) < 1024 {
				t.Errorf("PNG suspiciously small: %d bytes", len(b))
			}
			cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
			if err != nil {
				t.Fatalf("decode config: %v", err)
			}
			if cfg.Width != 1016 { // 508 card units rendered at 2x
				t.Errorf("width = %d, want 1016", cfg.Width)
			}
		})
	}
}

func TestRenderCardSVG(t *testing.T) {
	out := filepath.Join(t.TempDir(), "card.svg")
	if err := renderCard(sampleModel(), "dots", "svg", out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "<svg") {
		t.Errorf("output does not look like SVG")
	}
	for _, hex := range []string{"#FF6B6B", "#FFB454", "#4EE88E"} {
		if !strings.Contains(strings.ToUpper(s), hex) {
			t.Errorf("SVG missing tier colour %s", hex)
		}
	}
}

func TestRenderCardInvalidFormat(t *testing.T) {
	out := filepath.Join(t.TempDir(), "card.gif")
	if err := renderCard(sampleModel(), "dots", "gif", out); err == nil {
		t.Fatal("expected an error for an unsupported format")
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("no file should be written on invalid format")
	}
}
