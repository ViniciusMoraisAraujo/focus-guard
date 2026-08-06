// Package filelog redirects the standard logger to a file (append mode, with
// size-based rotation), shared by every FocusGuard binary so the daemon,
// watchdog, tray and web all write their <name>.log to the same folder —
// next to the executable in the install directory.
package filelog

import (
	"log"
	"os"
	"path/filepath"
)

// DefaultMaxSize is the size (in bytes) at which a log file is rotated to
// <name>.1 before a fresh file is opened on the next startup.
const DefaultMaxSize int64 = 1 << 20 // 1 MiB

// Setup redirects the standard logger to path (append mode, with size-based
// rotation at maxSize), keeping date+time flags. It returns a func that
// restores the previous output and flags, plus an error when the file cannot
// be opened (the logger then stays untouched).
func Setup(path string, maxSize int64) (func(), error) {
	rotate(path, maxSize)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return func() {}, err
	}
	prevOut := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(f)
	log.SetFlags(prevFlags | log.LstdFlags)
	return func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
		_ = f.Close()
	}, nil
}

// rotate moves an oversized log file to <name>.1 (replacing an old one) so
// the next open starts a fresh file. Best-effort: rotation failures only cost
// the log staying large.
func rotate(path string, maxSize int64) {
	info, err := os.Stat(path)
	if err != nil || info.Size() < maxSize {
		return
	}
	_ = os.Remove(path + ".1")
	_ = os.Rename(path, path+".1")
}

// PathFor returns fileName inside the directory of the given executable —
// the shared log folder for every FocusGuard binary (C:\Program Files\FocusGuard
// on Windows, /opt/focusguard on Linux).
func PathFor(exe, fileName string) string {
	return filepath.Join(filepath.Dir(exe), fileName)
}
