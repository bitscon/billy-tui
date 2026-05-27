package main

import (
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
)

type model struct {
	width                int
	height               int
	chatViewport         viewport.Model
	sidebarWidth         int
	input                textinput.Model
	messages             []string
	displayMessages      []string
	ready                bool
	spinner              spinner.Model
	thinking             bool
	client               *billyClient
	sidebar              sidebarState
	governanceAlertTicks int
	lastGovernanceEvent  string
	saveStatus           string
	saveStatusTicks      int
	isStreaming     bool
	streamBuffer    string
}

type responseMsg struct{ text string }
type errMsg struct{ text string }
type healthResultMsg struct{ err error }
type StreamChunkMsg struct {
	Chunk  string
	Prompt string
	events <-chan streamEvent
}
type StreamDoneMsg struct{ FullText string }
type StreamErrMsg struct {
	Prompt string
	Err    error
}

func initialModel(client *billyClient) model {
	ti := textinput.New()
	ti.Placeholder = "Type a message..."
	ti.Focus()

	sp := spinner.New(spinner.WithSpinner(spinner.Dot))

	return model{
		messages: []string{
			"[Billy] Hey. What can I build for you?",
		},
		displayMessages: []string{
			BillyResponseStyle.Render("[Billy] ") + "Hey. What can I build for you?",
		},
		input:   ti,
		spinner: sp,
		client:  client,
	}
}
