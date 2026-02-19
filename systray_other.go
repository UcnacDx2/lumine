//go:build !windows

package main

// runWithSystray is not available on non-Windows platforms
func runWithSystray(mainFunc func()) {
	// On non-Windows platforms, just run the main function directly
	mainFunc()
}
