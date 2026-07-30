//go:build !windows

package main

func tryRunAsService() bool {
	return false
}
