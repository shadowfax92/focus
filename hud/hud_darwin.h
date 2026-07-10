#ifndef HUD_DARWIN_H
#define HUD_DARWIN_H

void hudInit(double idleOpacity, const char *posPreset, double posX, double posY,
             int pulseSeconds);
void hudRunApp(void);
void hudSetFocus(const char *text, double sinceEpoch);
void hudClearFocus(void);
void hudPulse(int rung);
void hudShowTakeover(const char *focusText, const char *quote,
                     const char *mirrorLine, double gateSeconds);
void hudDismissTakeover(void);
void hudSetPaused(int paused);

// Test hooks for hud/demo only (not part of the frozen Go API): synthesize
// input through the real event path ([NSWindow sendEvent:]) so acks and drags
// are verifiable without OS-level event injection, which would require
// Accessibility permission.
void hudTestKey(unsigned short keyCode, const char *chars);
void hudTestPillClick(int optionHeld);
void hudTestPillDrag(double dx, double dy);
// Renders the pill/takeover view layers to PNGs (empty path = skip). Works
// without Screen Recording permission, unlike screencapture; NSVisualEffectView
// blur is a window-server composite and comes out dark, everything else is
// pixel-exact.
void hudTestSnapshot(const char *pillPath, const char *takeoverPath);

#endif
