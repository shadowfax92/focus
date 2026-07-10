//go:build !darwin || !cgo

package daemon

func idleSeconds() float64 { return 0 }
