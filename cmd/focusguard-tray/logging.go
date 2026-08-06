//go:build windows || (linux && cgo)

package main

import (
	"log"
	"os"

	"focusguard/internal/infrastructure/filelog"
)

// logFileName is the tray's log file, written next to the executable in the
// same folder as the daemon's log (C:\Program Files\FocusGuard on Windows,
// /opt/focusguard on Linux).
const logFileName = "focusguard-tray.log"

// maxLogSizeBeforeRotate is the size (in bytes) at which the log file is
// rotated to <name>.1 before a fresh file is opened on the next startup.
var maxLogSizeBeforeRotate int64 = filelog.DefaultMaxSize

// osExecutable é stubbable nos testes (o processo real usa os.Executable).
var osExecutable = os.Executable

// logPathFor returns the log file path next to the given executable.
func logPathFor(exe string) string {
	return filelog.PathFor(exe, logFileName)
}

// logPath returns the log file path next to the current executable. On error
// it falls back to the bare file name in the current directory.
func logPath() string {
	exe, err := osExecutable()
	if err != nil {
		return logFileName
	}
	return logPathFor(exe)
}

// setupLoggingAt redirects the standard logger to path (append mode, with
// size-based rotation), keeping date+time flags. It returns a func that
// restores the previous output and flags, plus an error when the file cannot
// be opened (the logger then stays untouched).
func setupLoggingAt(path string) (func(), error) {
	return filelog.Setup(path, maxLogSizeBeforeRotate)
}

// setupLogging wires the tray's log to the install directory (next to the
// executable), best-effort: on failure the standard logger stays on stderr
// and the problem is logged there. It returns a func that restores the
// previous output.
func setupLogging() func() {
	path := logPath()
	restore, err := setupLoggingAt(path)
	if err != nil {
		log.Printf("[FocusGuard Tray] Log em arquivo indisponível (%s): %v", path, err)
		return func() {}
	}
	return restore
}
