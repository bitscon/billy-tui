package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	addr := flag.String("addr", "", "billy-runtime address")
	flag.Parse()
	if *addr == "" {
		*addr = os.Getenv("BILLY_ADDR")
	}
	if *addr == "" {
		*addr = "http://localhost:5001"
	}

	client := newBillyClient(*addr)
	m := initialModel(client)

	// Start with mouse capture OFF so the terminal's native click-drag select and
	// copy works out of the box (you can mouse-copy Billy's replies). Capturing
	// the mouse would take that over; nothing here needs mouse events by default
	// and keyboard scroll covers navigation. ctrl+t toggles capture on at runtime
	// for anyone who prefers mouse-wheel scrolling.
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
