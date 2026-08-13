//go:build !windows && !linux

package filelog

// UserLogPath returns the bare file name in the current directory on
// platforms without a defined user state dir.
func UserLogPath(fileName string) string {
	return fileName
}
