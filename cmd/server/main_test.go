package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMainStartsAndExits(t *testing.T) {
	tmpDB := filepath.Join(t.TempDir(), "test.db")

	// Preserve and restore globals.
	oldArgs, oldStdin, oldStdout := os.Args, os.Stdin, os.Stdout
	t.Cleanup(func() {
		os.Args, os.Stdin, os.Stdout = oldArgs, oldStdin, oldStdout
	})

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open devnull: %v", err)
	}
	os.Stdin = devNull
	out, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open devnull for stdout: %v", err)
	}
	os.Stdout = out
	os.Args = []string{"server", "-db", tmpDB, "-org", "hashicorp", "-repo", "terraform-provider-azurerm"}

	main()
}
