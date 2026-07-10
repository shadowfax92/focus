package main

import (
	"os"
	"runtime"

	"github.com/shadowfax92/focus/cmd"
)

// Cocoa (hud.Run) must own the process main thread; lock it before the
// scheduler can move main() elsewhere.
func init() { runtime.LockOSThread() }

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
