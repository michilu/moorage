package main

import (
	"os"
	"testing"
)

func Test(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}
}

func TestLANGUAGE(t *testing.T) {
	language := os.Getenv("LANGUAGE")
	defer os.Setenv("LANGUAGE", language)

	os.Setenv("LANGUAGE", "ja_JP.UTF-8")
	lang := getLANGUAGE()
	if lang != "ja" {
		t.Error("is not `ja`: ", lang)
	}
}
