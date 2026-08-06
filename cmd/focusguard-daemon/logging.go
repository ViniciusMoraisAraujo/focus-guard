package main

import (
	"log"

	"focusguard/internal/infrastructure/filelog"
)

// logFileName is the daemon's log file, written next to the executable in the
// install directory (C:\Program Files\FocusGuard on Windows, /opt/focusguard
// on Linux) — the same folder the watchdog, tray and web logs go to.
const logFileName = "focusguard-daemon.log"

// maxLogSizeBeforeRotate is the size (in bytes) at which the daemon log file
// is rotated to <name>.1 before a fresh file is opened on the next startup.
var maxLogSizeBeforeRotate int64 = filelog.DefaultMaxSize

// logPathFor returns the daemon log file path next to the given executable.
func logPathFor(exe string) string {
	return filelog.PathFor(exe, logFileName)
}

// logPath returns the daemon log file path next to the current executable. On
// error it falls back to the bare file name in the current directory.
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

// setupLogging wires the daemon's log to the install directory (next to the
// executable), best-effort: on failure the standard logger stays on stderr and
// the problem is logged there. It returns a func that restores the previous
// output.
func setupLogging() func() {
	path := logPath()
	restore, err := setupLoggingAt(path)
	if err != nil {
		log.Printf("[FocusGuard Daemon] Log em arquivo indisponível (%s): %v", path, err)
		return func() {}
	}
	return restore
}
