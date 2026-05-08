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

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
