package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

// defaultSocketPath is Billy's kernel-verified Unix socket (billy-runtime STEP-717): over it
// the runtime knows the caller is the operator, so /model and the brain-floor writes work.
// The plain loopback web port is anonymous and refuses those writes.
const defaultSocketPath = "/home/billyb/.billy/sock/billy.sock"

// defaultAddr prefers the verified socket when it exists and falls back to the loopback port.
func defaultAddr(socketPath string) string {
	if fi, err := os.Stat(socketPath); err == nil && fi.Mode()&os.ModeSocket != 0 {
		return "unix://" + socketPath
	}
	return "http://localhost:5001"
}

func main() {
	addr := flag.String("addr", "", "billy-runtime address")
	flag.Parse()
	if *addr == "" {
		*addr = os.Getenv("BILLY_ADDR")
	}
	if *addr == "" {
		*addr = defaultAddr(defaultSocketPath)
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
