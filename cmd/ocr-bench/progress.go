package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type progressState struct {
	total, passed, failed int
	status                string
}

func (s progressState) summary() string {
	return fmt.Sprintf("%d/%d complete  |  Passed %d  |  Failed %d",
		s.passed+s.failed, s.total, s.passed, s.failed)
}

type progressModel struct {
	progressState
	width int
}

func (m progressModel) Init() tea.Cmd { return nil }

func (m progressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case progressState:
		m.progressState = msg
	case tea.WindowSizeMsg:
		m.width = msg.Width
	}
	return m, nil
}

func (m progressModel) View() string {
	width := min(48, max(8, m.width-6))
	filled := width * (m.passed + m.failed) / m.total
	bar := lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Render(strings.Repeat("━", filled)) +
		lipgloss.NewStyle().Faint(true).Render(strings.Repeat("─", width-filled))
	body := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Bold(true).Render("OCR benchmark  ·  "+m.status),
		bar,
		m.summary(),
	)
	if m.width < 54 {
		return body
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("39")).Padding(0, 1).Render(body)
}

type benchProgress struct {
	progressState
	program *tea.Program
	done    chan error
}

func newBenchProgress(total int) *benchProgress {
	p := &benchProgress{progressState: progressState{total: total, status: "Preparing"}}
	info, err := os.Stdout.Stat()
	if err == nil && info.Mode()&os.ModeCharDevice != 0 && os.Getenv("TERM") != "dumb" {
		p.program = tea.NewProgram(progressModel{progressState: p.progressState, width: 80},
			tea.WithInput(nil), tea.WithOutput(os.Stdout), tea.WithoutSignalHandler())
		p.done = make(chan error, 1)
		go func() {
			_, err := p.program.Run()
			p.done <- err
		}()
	}
	return p
}

func (p *benchProgress) show(status string) {
	p.status = status
	if p.program != nil {
		p.program.Send(p.progressState)
	}
}

func (p *benchProgress) record(passed bool) {
	if passed {
		p.passed++
	} else {
		p.failed++
	}
	p.show("Running")
}

func (p *benchProgress) print(w io.Writer, message string) {
	if p.program != nil {
		p.program.Println(strings.TrimRight(message, "\r\n"))
	} else {
		fmt.Fprintln(w, strings.TrimRight(message, "\r\n"))
	}
}

func (p *benchProgress) logf(format string, args ...any) {
	p.print(os.Stdout, fmt.Sprintf(format, args...))
}

func (p *benchProgress) aws(ctx context.Context, args ...string) ([]byte, error) {
	output, stderr, err := invokeAWS(ctx, args...)
	if len(stderr) != 0 {
		p.print(os.Stderr, string(stderr))
	}
	return output, err
}

func (p *benchProgress) finish(status string) {
	p.show(status)
	if p.program != nil {
		p.program.Quit()
		if err := <-p.done; err == nil {
			return
		}
	}
	fmt.Printf("OCR | %s | %s\n", p.summary(), status)
}
