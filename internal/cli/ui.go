package cli

import (
	"io"
	"os"

	"golang.org/x/term"
)

const (
	colorCyan   = "\x1b[36m"
	colorBlue   = "\x1b[34m"
	colorGreen  = "\x1b[32m"
	colorYellow = "\x1b[33m"
	colorRed    = "\x1b[31m"
	colorDim    = "\x1b[2m"
	colorBold   = "\x1b[1m"
	colorReset  = "\x1b[0m"
)

func paint(out io.Writer, style, text string) string {
	if !colorEnabled(out) {
		return text
	}
	return style + text + colorReset
}

func colorEnabled(out io.Writer) bool {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	file, ok := out.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

func heading(out io.Writer, text string) string {
	return paint(out, colorBold+colorCyan, text)
}

func success(out io.Writer, text string) string {
	return paint(out, colorBold+colorGreen, text)
}

func warning(out io.Writer, text string) string {
	return paint(out, colorYellow, text)
}

func label(out io.Writer, text string) string {
	return paint(out, colorDim, text)
}
