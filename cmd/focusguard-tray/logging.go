//go:build windows || (linux && cgo)

package main

import (
	"log"
	"os"
	"path/filepath"

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

// logPathFn / fallbackLogPathFn são stubbable nos testes. O fallback existe
// porque o diretório de instalação (C:\Program Files / /opt/focusguard) não é
// gravável por um processo user-space como o focusguard-tray — sem ele o log
// desapareceria (o stderr de um app GUI é invisível) e falhas de startup
// ficariam sem auditoria.
var (
	logPathFn         = logPath
	fallbackLogPathFn = fallbackLogPath
)

// fallbackLogPath returns a log path writable by the user-space tray when the
// install directory is not writable: the shared user state dir
// (filelog.UserLogPath — %PROGRAMDATA%\FocusGuard on Windows, XDG state
// ~/.local/state/focusguard no Linux).
func fallbackLogPath() string {
	return filelog.UserLogPath(logFileName)
}

// setupLogging wires the tray's log to the install directory (next to the
// executable); when that path is not writable — Program Files/opt sob um
// processo user-space sem elevação — it falls back to the shared user state
// dir so the audit trail is never silent. Best-effort: on total failure the
// standard logger stays on stderr and the problem is logged there. It
// returns a func that restores the previous output.
func setupLogging() func() {
	path := logPathFn()
	restore, err := setupLoggingAt(path)
	if err == nil {
		return restore
	}

	fallback := fallbackLogPathFn()
	if fallback == path {
		log.Printf("[FocusGuard Tray] Log em arquivo indisponível (%s): %v", path, err)
		return func() {}
	}
	if err := os.MkdirAll(filepath.Dir(fallback), 0o755); err != nil {
		log.Printf("[FocusGuard Tray] Log em arquivo indisponível (%s): %v (fallback %s: %v)", path, err, fallback, err)
		return func() {}
	}
	restore, fbErr := setupLoggingAt(fallback)
	if fbErr != nil {
		log.Printf("[FocusGuard Tray] Log em arquivo indisponível (%s): %v (fallback %s: %v)", path, err, fallback, fbErr)
		return func() {}
	}
	// Esta linha já sai para o arquivo recém-aberto: a primeira linha do log
	// de auditoria explica por que ele está ali (e não ao lado do exe).
	log.Printf("[FocusGuard Tray] Diretório de instalação sem permissão de escrita (%v) — log de auditoria em %s", err, fallback)
	return restore
}
