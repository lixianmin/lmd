package service

import (
	"testing"
)

func TestExtractKeywords(t *testing.T) {
	text := "I have a cat named Whiskers. Whiskers is a gray tabby. I feed Whiskers Fancy Feast brand food. My bedroom walls are light gray."
	kw := extractKeywords(text)

	hasWhiskers := false
	hasGray := false
	hasBedroom := false
	hasFancy := false
	for _, k := range kw {
		if k == "Whiskers" {
			hasWhiskers = true
		}
		if k == "gray" {
			hasGray = true
		}
		if k == "bedroom" {
			hasBedroom = true
		}
		if k == "Fancy" {
			hasFancy = true
		}
	}
	if !hasWhiskers {
		t.Fatalf("missing Whiskers in keywords: %v", kw)
	}
	if !hasGray {
		t.Fatalf("missing gray in keywords: %v", kw)
	}
	if !hasBedroom {
		t.Fatalf("missing bedroom in keywords: %v", kw)
	}
	if !hasFancy {
		t.Fatalf("missing Fancy in keywords: %v", kw)
	}
}

func TestExtractKeywordsEmpty(t *testing.T) {
	kw := extractKeywords("")
	if len(kw) != 0 {
		t.Fatalf("expected empty, got %v", kw)
	}
}

func TestExtractKeywordsStopWords(t *testing.T) {
	kw := extractKeywords("the a an is are was were")
	if len(kw) != 0 {
		t.Fatalf("expected empty (all stop words), got %v", kw)
	}
}
