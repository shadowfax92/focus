#ifndef HUD_DARWIN_H
#define HUD_DARWIN_H

void hudInit(double idleOpacity, const char *posPreset, double posX, double posY,
             int pulseSeconds);
void hudRunApp(void);
void hudSetFocus(const char *text, double sinceEpoch, long long budgetNanos);
void hudClearFocus(void);
// reminderID correlates passive glow clicks; 0 keeps the pulse ladder path.
void hudPulse(int rung, unsigned long long reminderID);
void hudShowTakeover(const char *focusText, const char *quote,
                     const char *mirrorLine, int rung, double gateSeconds);
void hudDismissTakeover(void);
void hudSetPaused(int paused);

// Test hooks for hud/demo only (not part of the frozen Go API): synthesize
// input through the real event path ([NSWindow sendEvent:]) while honoring the
// window's ignoresMouseEvents state, so acks and drags are verifiable without
// OS-level event injection, which would require Accessibility permission.
void hudTestKey(unsigned short keyCode, const char *chars);
// optionHeld only selects the drifted acknowledgement during an active pulse;
// ambient dragging never requires it.
void hudTestPillClick(int optionHeld);
void hudTestPillDrag(double dx, double dy);
double hudTestPillAlpha(void);
// Writes PNGs without Screen Recording permission, unlike screencapture
// (empty path = skip). The pill is the window server's composite of its
// screen rect: the real glass, glow and window alpha over this process's own
// windows (other apps' windows may be omitted). If that capture is
// unavailable it falls back to a layer render, where glass comes out as a
// flat fill. The takeover is a layer render: its NSVisualEffectView blur
// comes out dark, everything else is pixel-exact.
void hudTestSnapshot(const char *pillPath, const char *takeoverPath);
// Shows an image in a window just under the pill, so a snapshot shows the
// glass over known dark or light content.
void hudTestBackdrop(const char *imagePath);

#endif
