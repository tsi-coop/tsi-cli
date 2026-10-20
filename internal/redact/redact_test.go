package redact

import "testing"

func TestStringAndMask(t *testing.T) {
	Add("super-secret-value", "x")
	if got := String("token=super-secret-value!"); got != "token=********!" {
		t.Fatalf("got %q", got)
	}
	if got := String("an x in text"); got != "an x in text" {
		t.Fatalf("short values must not be masked: %q", got)
	}
	if Mask("abcdefghijkl") != "abcd…ijkl" || Mask("short") != "********" {
		t.Fatal("mask")
	}
}
