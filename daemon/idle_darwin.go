//go:build darwin && cgo

package daemon

/*
#cgo LDFLAGS: -framework CoreGraphics
#include <CoreGraphics/CoreGraphics.h>
*/
import "C"

func idleSeconds() float64 {
	return float64(C.CGEventSourceSecondsSinceLastEventType(
		C.kCGEventSourceStateCombinedSessionState,
		C.kCGAnyInputEventType,
	))
}
