package main

import (
	"path/filepath"
	"testing"
)

func TestSharedDebugDirIsUsedWhenItExists(t *testing.T) {
	dir := t.TempDir()
	if got, ok := sharedDebugDirFor("", dir); !ok || got != dir {
		t.Fatalf("got %q %v", got, ok)
	}
}

func TestSharedDebugDirYieldsToTheOverride(t *testing.T) {
	if _, ok := sharedDebugDirFor("/somewhere", t.TempDir()); ok {
		t.Fatal("the BILLY_DEBUG_DIR override must win")
	}
}

func TestSharedDebugDirAbsentFallsBackToHome(t *testing.T) {
	if _, ok := sharedDebugDirFor("", filepath.Join(t.TempDir(), "absent")); ok {
		t.Fatal("an absent shared directory must fall back to ~/.billy/debug")
	}
}
