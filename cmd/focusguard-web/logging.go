package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"

	"focusguard/internal/infrastructure/filelog"
)

// logFileName is the web server's log file, written next to the executable
// in the same folder as the daemon's log (C:\Program Files\FocusGuard on
// Windows, /opt/focusguard on Linux).
const logFileName = "focusguard-web.log"

// maxLogSizeBeforeRotate is the size (in bytes) at which the log file is
// rotated to <name>.1 before a fresh file is opened on the next startup.
var maxLogSizeBeforeRotate int64 = filelog.DefaultMaxSize

// osExecutable é stubbable nos testes (o processo real usa os.Executable).
var osExecutable = os.Executable

// logPathFn / fallbackLogPathFn são stubbable nos testes. O fallback existe
// porque o diretório de instalação (C:\Program Files) não é gravável por um
// processo user-space como o focusguard-web — sem ele o log desapareceria
// (o stderr é descartado pelo spawner do CLI) e falhas de startup como o
// bind em uso ficariam invisíveis para auditoria.
var (
	logPathFn         = logPath
	fallbackLogPathFn = fallbackLogPath
)

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

// fallbackLogPath returns a log path writable by the user-space web process
// when the install directory (next to the executable) is not writable: the
// shared state dir, the same place the daemon keeps state.json
// (%PROGRAMDATA%\FocusGuard on Windows, /var/lib/focusguard on Linux).
func fallbackLogPath() string {
	if runtime.GOOS == "windows" {
		pd := os.Getenv("PROGRAMDATA")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		return filepath.Join(pd, "FocusGuard", logFileName)
	}
	if runtime.GOOS == "linux" {
		return filepath.Join("/var/lib/focusguard", logFileName)
	}
	return logFileName
}

// setupLoggingAt redirects the standard logger to path (append mode, with
// size-based rotation), keeping date+time flags. It returns a func that
// restores the previous output and flags, plus an error when the file cannot
// be opened (the logger then stays untouched).
func setupLoggingAt(path string) (func(), error) {
	return filelog.Setup(path, maxLogSizeBeforeRotate)
}

// setupLogging wires the web server's log to the install directory (next to
// the executable); when that path is not writable — Program Files sob um
// processo user-space sem elevação — it falls back to the shared state dir so
// the audit trail is never silent. Best-effort: on total failure the standard
// logger stays on stderr and the problem is logged there.
func setupLogging() func() {
	path := logPathFn()
	restore, err := setupLoggingAt(path)
	if err == nil {
		return restore
	}

	fallback := fallbackLogPathFn()
	if fallback == path {
		log.Printf("[focusguard-web] Log em arquivo indisponível (%s): %v", path, err)
		return func() {}
	}
	if err := os.MkdirAll(filepath.Dir(fallback), 0o755); err != nil {
		log.Printf("[focusguard-web] Log em arquivo indisponível (%s): %v (fallback %s: %v)", path, err, fallback, err)
		return func() {}
	}
	restore, fbErr := setupLoggingAt(fallback)
	if fbErr != nil {
		log.Printf("[focusguard-web] Log em arquivo indisponível (%s): %v (fallback %s: %v)", path, err, fallback, fbErr)
		return func() {}
	}
	// Esta linha já sai para o arquivo recém-aberto: a primeira linha do log
	// de auditoria explica por que ele está ali (e não ao lado do exe).
	log.Printf("[focusguard-web] Diretório de instalação sem permissão de escrita (%v) — log de auditoria em %s", err, fallback)
	return restore
}
