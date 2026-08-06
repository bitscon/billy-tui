package main

import "testing"

// The model picker must window a long list to the terminal height, keep the
// selected row visible, and clamp to the option count — otherwise a host with
// many models (e.g. Ollama) overflows the screen and the selection can move
// below the fold.
func TestModelPickerWindow(t *testing.T) {
	tests := []struct {
		name               string
		idx, total, h      int
		wantStart, wantEnd int
	}{
		// Short list on a tall terminal: show everything, no windowing.
		{"fits entirely", 0, 5, 40, 0, 5},
		// Long list, selection at top: window anchored at 0.
		{"top of long list", 0, 100, 30, 0, 20},
		// Long list, selection at bottom: window ends at total.
		{"bottom of long list", 99, 100, 30, 80, 100},
		// Long list, selection in the middle: window centered on idx.
		{"middle of long list", 50, 100, 30, 40, 60},
		// Very short terminal still shows at least 3 rows.
		{"short terminal floor", 10, 100, 8, 9, 12},
		// Empty list is safe.
		{"empty", 0, 0, 30, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			start, end := modelPickerWindow(tc.idx, tc.total, tc.h)
			if start != tc.wantStart || end != tc.wantEnd {
				t.Fatalf("modelPickerWindow(%d,%d,%d) = [%d,%d), want [%d,%d)",
					tc.idx, tc.total, tc.h, start, end, tc.wantStart, tc.wantEnd)
			}
			// The selected index must always fall inside the window (for a
			// non-empty list).
			if tc.total > 0 && (tc.idx < start || tc.idx >= end) {
				t.Fatalf("selected idx %d outside window [%d,%d)", tc.idx, start, end)
			}
			// The window never runs past the option list.
			if end > tc.total {
				t.Fatalf("window end %d exceeds total %d", end, tc.total)
			}
		})
	}
}

// The selected row stays visible as it walks the whole list — the property that
// actually matters for usability, checked across every index.
func TestModelPickerWindowKeepsSelectionVisible(t *testing.T) {
	const total, h = 200, 24
	for idx := 0; idx < total; idx++ {
		start, end := modelPickerWindow(idx, total, h)
		if idx < start || idx >= end {
			t.Fatalf("idx %d not visible in window [%d,%d)", idx, start, end)
		}
		if start < 0 || end > total {
			t.Fatalf("window [%d,%d) out of bounds for total %d", start, end, total)
		}
	}
}
