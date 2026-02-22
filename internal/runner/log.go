package runner

import (
	"fmt"
	"os"
)

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorDim    = "\033[2m"
)

type logger struct {
	silent  bool
	verbose bool
	noColor bool
}

var log = &logger{}

func configureLogger(silent, verbose, noColor bool) {
	log.silent = silent
	log.verbose = verbose
	log.noColor = noColor
}

func (l *logger) Info(format string, args ...any) {
	if l.silent {
		return
	}
	msg := fmt.Sprintf(format, args...)
	if l.noColor {
		fmt.Fprintf(os.Stderr, "[INF] %s\n", msg)
	} else {
		fmt.Fprintf(os.Stderr, "%s[INF]%s %s\n", colorBlue, colorReset, msg)
	}
}

func (l *logger) Warning(format string, args ...any) {
	if l.silent {
		return
	}
	msg := fmt.Sprintf(format, args...)
	if l.noColor {
		fmt.Fprintf(os.Stderr, "[WRN] %s\n", msg)
	} else {
		fmt.Fprintf(os.Stderr, "%s[WRN]%s %s\n", colorYellow, colorReset, msg)
	}
}

func (l *logger) Error(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if l.noColor {
		fmt.Fprintf(os.Stderr, "[ERR] %s\n", msg)
	} else {
		fmt.Fprintf(os.Stderr, "%s[ERR]%s %s\n", colorRed, colorReset, msg)
	}
}

func (l *logger) Fatal(format string, args ...any) {
	l.Error(format, args...)
	os.Exit(1)
}

func (l *logger) Debug(format string, args ...any) {
	if !l.verbose {
		return
	}
	msg := fmt.Sprintf(format, args...)
	if l.noColor {
		fmt.Fprintf(os.Stderr, "[DBG] %s\n", msg)
	} else {
		fmt.Fprintf(os.Stderr, "%s[DBG] %s%s\n", colorDim, msg, colorReset)
	}
}

func (l *logger) Success(format string, args ...any) {
	if l.silent {
		return
	}
	msg := fmt.Sprintf(format, args...)
	if l.noColor {
		fmt.Fprintf(os.Stderr, "[OK] %s\n", msg)
	} else {
		fmt.Fprintf(os.Stderr, "%s[OK]%s %s\n", colorGreen, colorReset, msg)
	}
}

func logWriter() *os.File {
	return os.Stderr
}
