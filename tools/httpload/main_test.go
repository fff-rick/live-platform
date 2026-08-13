package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadTokensCombinesSingleAndFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.txt")
	if err := os.WriteFile(path, []byte(" token-a \n\n token-b\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := loadTokens("single", path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"single", "token-a", "token-b"}
	if len(got) != len(want) {
		t.Fatalf("len=%d want=%d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("token[%d]=%q want=%q", i, got[i], want[i])
		}
	}
}
