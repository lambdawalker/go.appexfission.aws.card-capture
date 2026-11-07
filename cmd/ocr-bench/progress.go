package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

type benchProgress struct {
	out                   io.Writer
	interactive           bool
	total, passed, failed int
	lineWidth             int
}

func newBenchProgress(total int) *benchProgress {
	info, err := os.Stdout.Stat()
	return &benchProgress{
		out:         os.Stdout,
		interactive: err == nil && info.Mode()&os.ModeCharDevice != 0 && os.Getenv("TERM") != "dumb",
		total:       total,
	}
}

func (p *benchProgress) line(status string) string {
	const barWidth = 14
	done := p.passed + p.failed
	filled := done * barWidth / p.total
	return fmt.Sprintf("OCR | [%s%s] %d/%d | Passed %d | Failed %d | %s",
		strings.Repeat("#", filled), strings.Repeat("-", barWidth-filled),
		done, p.total, p.passed, p.failed, status)
}

func (p *benchProgress) show(status string) {
	if !p.interactive {
		return
	}
	p.clear()
	line := p.line(status)
	fmt.Fprint(p.out, line)
	p.lineWidth = len(line)
}

func (p *benchProgress) clear() {
	if p.lineWidth != 0 {
		fmt.Fprintf(p.out, "\r%s\r", strings.Repeat(" ", p.lineWidth))
		p.lineWidth = 0
	}
}

func (p *benchProgress) record(passed bool) {
	if passed {
		p.passed++
	} else {
		p.failed++
	}
	p.show("Completed")
}

func (p *benchProgress) finish(status string) {
	p.clear()
	fmt.Fprintln(p.out, p.line(status))
}
