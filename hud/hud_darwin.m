// focus HUD: ambient glow pill + pulse ladder + full-screen ack takeover.
// The pill is Liquid Glass (NSGlassEffectView on macOS 26) with an amber glow,
// per the owner-approved Paper design "02 · Liquid"; the takeover keeps the
// visual language lifted from mac-notify's overlay (dark panel, cyan glow).
// Both animate with generation-guarded breathing loops. Memory model is MRC
// (no ARC flags in the cgo build): long-lived views are created once and
// retained forever; per-show strings are strdup'd outside the main-queue hop
// and freed inside.
#import <AppKit/AppKit.h>
#import <QuartzCore/QuartzCore.h>
#include <dlfcn.h>
#include <stdlib.h>
#include <string.h>
#include "hud_darwin.h"

// Implemented in hud_darwin.go (//export).
extern void goHudAck(int kind, int rung, double latencySeconds, const char *newText);
extern void goHudPassivePulseAck(int kind, unsigned long long reminderID, double latencySeconds);
extern void goHudMoved(double x, double y);
extern void goHudFormatPillTime(double elapsedSeconds, long long budgetNanos,
                              char **elapsed, char **budget, char **overage);

// Mirrors hud.AckKind iota order.
enum { kAckOnTask = 0, kAckDrifted = 1, kAckRefocus = 2, kAckDone = 3 };

static const CGFloat kPillWidth = 460;
// Liquid layout: focus text on the left, a nested time chip hugging the right
// rim. A single-line pill is a 46pt capsule; wrapped text grows it downward
// as a rounded rect with the chip centered.
static const CGFloat kPillPadLeft = 24;
static const CGFloat kPillPadRight = 7;
static const CGFloat kPillPadY = 7;
static const CGFloat kPillTextPadY = 11; // keeps wrapped lines off the corners
static const CGFloat kPillGap = 16;      // focus text -> chip
static const CGFloat kChipH = 32;
static const CGFloat kChipPadX = 14;
static const CGFloat kChipGap = 6;       // elapsed -> budget -> overage
static const CGFloat kChipOverEndPad = 6;
// The overage sub-chip's white ring is drawn as an inside border, so its
// frame grows by the ring width to keep the red fill at Paper's size.
static const CGFloat kOverRing = 1.5;
static const CGFloat kOverPadX = 9;
static const CGFloat kOverH = 20 + 2 * kOverRing;
static const CGFloat kPillMaxRadius = (kChipH + 2 * kPillPadY) / 2;
// Transparent margin around the visual panel so the glow and drop shadow
// have room to render instead of clipping at the window edge.
static const CGFloat kGlowPad = 44;
static const CGFloat kTopGap = 8;
static const CGFloat kSideMargin = 16;

// --- forward declarations -------------------------------------------------

static void layoutPill(void);
static void refreshPillVisibility(void);
static void updateInteractivity(void);
static void stylePill(void);
static void pillBreathe(int gen, BOOL expand);
static void endPulseNow(void);
static void killPulseSilent(void);
static void pillAck(int kind);
static void layoutTakeover(void);
static void showTakeoverMain(NSString *focus, NSString *quote, NSString *mirror, int rung, double gate);
static void dismissTakeoverMain(void);
static void armTakeover(void);
static void takeoverAck(int kind, NSString *newText);
static void beginRetype(BOOL doneMode);
static void endRetype(void);
static void startCircleBreathing(void);
static NSAttributedString *armedHintString(void);
static NSAttributedString *editHintString(void);
static NSAttributedString *doneHintString(void);

static NSColor *cyan(CGFloat alpha) {
    return [NSColor colorWithRed:0 green:0.85 blue:1.0 alpha:alpha];
}

// Pill colour roles (owner feedback on the Paper variants): amber only ever
// means "look at me" (the glow), and over-budget is a redder red with a white
// ring so the two signals can never be mistaken for each other.
static NSColor *amber(CGFloat alpha) {
    return [NSColor colorWithRed:1.0 green:0.667 blue:0.157 alpha:alpha]; // #FFAA28
}

static NSColor *amberRim(CGFloat alpha) {
    return [NSColor colorWithRed:1.0 green:0.784 blue:0.431 alpha:alpha]; // #FFC86E
}

static NSColor *overRed(CGFloat alpha) {
    return [NSColor colorWithRed:1.0 green:0.267 blue:0.2 alpha:alpha]; // #FF4433
}

// Ink adapts to the glass: white over dark backdrops, near-black over light.
static NSColor *pillInk(BOOL dark, CGFloat alpha) {
    if (dark) return [NSColor colorWithWhite:1.0 alpha:alpha];
    return [NSColor colorWithRed:0.043 green:0.043 blue:0.055 alpha:alpha]; // #0B0B0E
}

static double nowSec(void) {
    return [[NSDate date] timeIntervalSince1970];
}

static double machTime(void) {
    return [[NSProcessInfo processInfo] systemUptime];
}

// Wrapped height of an attributed string at the given width (mac-notify's
// measureBodyHeight generalized to attributed strings).
static CGFloat measureAttrHeight(NSAttributedString *attr, CGFloat width) {
    NSTextStorage *ts = [[NSTextStorage alloc] initWithAttributedString:attr];
    NSTextContainer *tc = [[NSTextContainer alloc] initWithSize:NSMakeSize(width, CGFLOAT_MAX)];
    NSLayoutManager *lm = [[NSLayoutManager alloc] init];
    tc.lineFragmentPadding = 0;
    [lm addTextContainer:tc];
    [ts addLayoutManager:lm];
    [lm glyphRangeForTextContainer:tc];
    CGFloat h = ceil([lm usedRectForTextContainer:tc].size.height);
    [ts release];
    [tc release];
    [lm release];
    return h < 20 ? 20 : h;
}

static CGFloat measureStringHeight(NSString *s, NSFont *font, CGFloat width) {
    NSAttributedString *attr = [[[NSAttributedString alloc]
        initWithString:(s ?: @"") attributes:@{NSFontAttributeName: font}] autorelease];
    return measureAttrHeight(attr, width);
}

static BOOL rectOnAnyScreen(NSRect r) {
    for (NSScreen *s in [NSScreen screens]) {
        if (NSIntersectsRect(r, s.visibleFrame)) return YES;
    }
    return NO;
}

// --- classes ----------------------------------------------------------------

@interface PillView : NSView {
    NSPoint dragStartScreen;
    NSPoint dragStartOrigin;
    BOOL didDrag;
}
@end

// The glass's content view. NSGlassEffectView picks a light or dark appearance
// from whatever is behind it and pushes it down here, so this is where the
// pill learns to switch ink when it drifts from a dark editor onto a white page.
@interface PillContentView : NSView
@end

// Amber wash inside the glass for the glow; its backing layer is a gradient.
@interface PillTintView : NSView
@end

// Spotlight pattern: a nonactivating borderless panel that can still become
// key, so the takeover swallows every keystroke without activating the app —
// and key focus snaps back to the previous app when it orders out.
@interface KeyPanel : NSPanel
@end

@interface KeyCatcherView : NSView
@end

@interface HudController : NSObject <NSTextFieldDelegate, NSWindowDelegate>
@end

// --- state ------------------------------------------------------------------

static double _idleOpacity = 0.30;
static NSString *_posPreset = nil;
static double _posX = 0, _posY = 0;
static int _pulseSeconds = 8;

// Pill view tree (window = panel inset by -kGlowPad):
//   _pillRoot (PillView: drag + ack clicks for the whole window)
//     _pillDrop   static dark drop shadow, masked to outside the glass
//     _pillGlow   amber halo that breathes, masked to outside the glass
//     _pillGlass  NSGlassEffectView (NSVisualEffectView before macOS 26)
//       _pillContent (PillContentView)
//         _pillTint   amber wash + rim that breathes with the halo
//         _pillLabel  focus text
//         _pillChip   time chip: _pillTimeLabel + _pillOver(_pillOverLabel)
// Effect views mask their content to their own bounds, so anything that must
// glow outside the glass lives in a sibling view behind it.
static NSPanel *_pill = nil;
static PillView *_pillRoot = nil;
static NSView *_pillDrop = nil;
static NSView *_pillGlow = nil;
static NSView *_pillGlass = nil;
static PillContentView *_pillContent = nil;
static PillTintView *_pillTint = nil;
static NSTextField *_pillLabel = nil;
static NSView *_pillChip = nil;
static NSTextField *_pillTimeLabel = nil;
static NSView *_pillOver = nil;
static NSTextField *_pillOverLabel = nil;
// Go's chip runs as of the last layout. Restyling on an appearance flip reuses
// them, so the text can't change width without a matching relayout.
static NSString *_pillElapsed = nil;
static NSString *_pillBudget = nil;
static NSString *_pillOverage = nil;
static NSString *_focusText = nil;
static double _sinceEpoch = 0;
static long long _budgetNanos = 0;
static BOOL _focusSet = NO;
static BOOL _paused = NO;
static BOOL _pulsing = NO;
static int _rung = 0;
static int _pulseGen = 0;
static double _pulseShownAt = 0;
// Daemon correlation token, distinct from _pulseGen's animation cancellation.
// Zero selects the existing ladder callback; passive glows echo a nonzero ID.
static unsigned long long _pulseReminderID = 0;
// Screen y of the pill panel's top edge; text growth extends downward from it.
static CGFloat _pillTop = -1;
static NSTimer *_elapsedTimer = nil;

static KeyPanel *_tk = nil;
static KeyCatcherView *_tkRoot = nil;
static NSVisualEffectView *_tkBlur = nil;
static NSView *_tkDim = nil;
static NSTextField *_tkFocus = nil;
static NSTextField *_tkQuote = nil;
static NSTextField *_tkMirror = nil;
static NSTextField *_tkHints = nil;
static NSView *_tkCircleHolder = nil;
static CAShapeLayer *_tkCircle = nil;
static NSView *_tkFieldBox = nil;
static NSTextField *_tkField = nil;
static HudController *_controller = nil;
static BOOL _tkVisible = NO;
static BOOL _tkArmed = NO;
static BOOL _tkEditing = NO;
static BOOL _tkDoneMode = NO;
static int _tkGen = 0;
static double _tkShownAt = 0;
static int _tkRung = 0;

// --- pill -------------------------------------------------------------------

@implementation PillView
// Glass internals and labels must never consume the drag or the ack click:
// every hit inside the pill window lands here.
- (NSView *)hitTest:(NSPoint)point {
    return [super hitTest:point] ? self : nil;
}
- (void)mouseDown:(NSEvent *)event {
    dragStartScreen = [self.window convertPointToScreen:event.locationInWindow];
    dragStartOrigin = self.window.frame.origin;
    didDrag = NO;
}
- (void)mouseDragged:(NSEvent *)event {
    NSPoint s = [self.window convertPointToScreen:event.locationInWindow];
    CGFloat dx = s.x - dragStartScreen.x;
    CGFloat dy = s.y - dragStartScreen.y;
    if (!didDrag && (fabs(dx) > 3 || fabs(dy) > 3)) didDrag = YES;
    if (didDrag) {
        [self.window setFrameOrigin:NSMakePoint(dragStartOrigin.x + dx, dragStartOrigin.y + dy)];
    }
}
- (void)mouseUp:(NSEvent *)event {
    if (didDrag) {
        NSRect wf = self.window.frame;
        // Report the visual panel origin, not the oversized glow window's.
        NSPoint panelOrigin = NSMakePoint(wf.origin.x + kGlowPad, wf.origin.y + kGlowPad);
        [_posPreset release];
        _posPreset = [@"custom" retain];
        _posX = panelOrigin.x;
        _posY = panelOrigin.y;
        _pillTop = panelOrigin.y + (wf.size.height - 2 * kGlowPad);
        goHudMoved(panelOrigin.x, panelOrigin.y);
        return;
    }
    BOOL opt = (event.modifierFlags & NSEventModifierFlagOption) != 0;
    pillAck(opt ? kAckDrifted : kAckOnTask);
}
@end

@implementation PillContentView
- (void)viewDidChangeEffectiveAppearance {
    [super viewDidChangeEffectiveAppearance];
    stylePill();
}
@end

@implementation PillTintView
- (CALayer *)makeBackingLayer {
    return [CAGradientLayer layer];
}
@end

// Glow look per rung. The halo radius/opacity, tint alpha and rim are visual;
// period is pulse timing and must not change with a restyle. Rung 0 is the
// frequent passive nudge (every pulse_interval), so it stays gentle; rung 2 is
// the owner's hard ceiling — no rung may glow brighter or wider than it.
typedef struct {
    CGFloat radiusMin, radiusMax;   // amber halo blur outside the glass
    float opacityMin, opacityMax;   // amber halo opacity
    CGFloat tintMin, tintMax;       // alpha of the amber wash inside the glass
    CGFloat borderWidth;            // amber rim, fades with the wash
    CGFloat borderAlpha;
    double period;
} GlowSpec;

static GlowSpec glowForRung(int rung) {
    if (rung <= 0) return (GlowSpec){5, 13, 0.30f, 0.55f, 0.35, 0.75, 1.0, 0.55, 1.0};
    if (rung == 1) return (GlowSpec){8, 16, 0.40f, 0.60f, 0.50, 0.85, 1.25, 0.75, 0.65};
    return (GlowSpec){14, 20, 0.55f, 0.65f, 0.80, 1.00, 1.5, 0.90, 0.50};
}

static const CGFloat kGlowRestRadius = 5;
// Paper's box-shadow spread: the halo starts slightly outside the rim.
static const CGFloat kGlowSpread = 2;

static BOOL pillIsDark(void) {
    NSAppearance *a = _pillContent ? _pillContent.effectiveAppearance : NSApp.effectiveAppearance;
    NSString *best = [a bestMatchFromAppearancesWithNames:@[NSAppearanceNameAqua, NSAppearanceNameDarkAqua]];
    return [best isEqualToString:NSAppearanceNameDarkAqua];
}

static NSAttributedString *pillFocusAttr(BOOL dark) {
    // Dark glass gets a soft drop shadow under white ink; light glass gets a
    // white halo under dark ink. Either way the text lifts off busy content.
    NSShadow *shadow = [[[NSShadow alloc] init] autorelease];
    shadow.shadowOffset = dark ? NSMakeSize(0, -1) : NSZeroSize;
    shadow.shadowBlurRadius = dark ? 3 : 8;
    shadow.shadowColor = dark ? [NSColor colorWithWhite:0 alpha:0.45] : [NSColor colorWithWhite:1 alpha:0.7];
    return [[[NSAttributedString alloc]
        initWithString:(_focusText ?: @"")
            attributes:@{
                NSFontAttributeName: [NSFont systemFontOfSize:18 weight:NSFontWeightHeavy],
                NSKernAttributeName: @(-0.27),
                NSForegroundColorAttributeName: pillInk(dark, 1.0),
                NSShadowAttributeName: shadow,
            }] autorelease];
}

static NSAttributedString *pillTimeAttr(BOOL dark) {
    NSMutableAttributedString *s = [[[NSMutableAttributedString alloc] init] autorelease];
    NSString *elapsed = _pillElapsed ?: @"";
    [s appendAttributedString:[[[NSAttributedString alloc]
        initWithString:elapsed
            attributes:@{
                NSFontAttributeName: [NSFont monospacedDigitSystemFontOfSize:15 weight:NSFontWeightBold],
                NSForegroundColorAttributeName: pillInk(dark, 1.0),
            }] autorelease]];
    if (_pillBudget.length && elapsed.length) {
        // Kern on elapsed's last glyph is the gap, so no space glyph is measured.
        [s addAttribute:NSKernAttributeName value:@(kChipGap) range:NSMakeRange(elapsed.length - 1, 1)];
        [s appendAttributedString:[[[NSAttributedString alloc]
            initWithString:_pillBudget
                attributes:@{
                    NSFontAttributeName: [NSFont monospacedDigitSystemFontOfSize:15 weight:NSFontWeightMedium],
                    NSForegroundColorAttributeName: pillInk(dark, dark ? 0.58 : 0.50),
                }] autorelease]];
    }
    return s;
}

static NSAttributedString *pillOverAttr(void) {
    return [[[NSAttributedString alloc]
        initWithString:(_pillOverage ?: @"")
            attributes:@{
                NSFontAttributeName: [NSFont monospacedDigitSystemFontOfSize:14 weight:NSFontWeightHeavy],
                NSForegroundColorAttributeName: [NSColor whiteColor],
            }] autorelease];
}

static NSTextField *makePillLabel(NSView *parent) {
    NSTextField *l = [[NSTextField labelWithString:@""] retain];
    [parent addSubview:l];
    return l;
}

// Sizes a one-line label to its text and puts the glyphs (not the cell's
// internal inset) at textX, vertically centered on midY.
static void placeLabel(NSTextField *l, NSAttributedString *attr, CGFloat textX, CGFloat midY) {
    l.attributedStringValue = attr;
    [l sizeToFit];
    NSSize fit = l.frame.size;
    CGFloat inset = (fit.width - ceil(attr.size.width)) / 2;
    l.frame = NSMakeRect(textX - inset, round(midY - fit.height / 2), fit.width, fit.height);
}

static NSView *makeShadowView(CGColorRef color, CGFloat radius, float opacity, CGSize offset) {
    NSView *v = [[NSView alloc] initWithFrame:_pillRoot.bounds];
    v.wantsLayer = YES;
    v.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
    CALayer *layer = v.layer;
    layer.shadowColor = color;
    layer.shadowRadius = radius;
    layer.shadowOpacity = opacity;
    layer.shadowOffset = offset;
    CAShapeLayer *mask = [CAShapeLayer layer];
    mask.fillRule = kCAFillRuleEvenOdd;
    layer.mask = mask;
    return v;
}

// The halo and drop shadow are drawn from shadowPath alone, then masked to
// everything outside the glass, so no colour ever bleeds under the glass.
static void shapeShadowView(NSView *v, NSRect glass, CGFloat radius, CGFloat spread) {
    CALayer *layer = v.layer;
    NSRect halo = NSInsetRect(glass, -spread, -spread);
    CGPathRef haloPath = CGPathCreateWithRoundedRect(halo, radius + spread, radius + spread, NULL);
    CGMutablePathRef outside = CGPathCreateMutable();
    CGPathAddRect(outside, NULL, v.bounds);
    CGPathAddRoundedRect(outside, NULL, glass, radius, radius);
    [CATransaction begin];
    [CATransaction setDisableActions:YES];
    layer.shadowPath = haloPath;
    CAShapeLayer *mask = (CAShapeLayer *)layer.mask;
    mask.frame = v.bounds;
    mask.path = outside;
    [CATransaction commit];
    CGPathRelease(haloPath);
    CGPathRelease(outside);
}

static void buildPill(void) {
    if (_pill) return;
    NSRect dummy = NSMakeRect(0, 0, kPillWidth + 2 * kGlowPad, 100);
    _pill = [[NSPanel alloc] initWithContentRect:dummy
                                       styleMask:NSWindowStyleMaskBorderless | NSWindowStyleMaskNonactivatingPanel
                                         backing:NSBackingStoreBuffered
                                           defer:NO];
    _pill.level = NSStatusWindowLevel + 1;
    _pill.opaque = NO;
    _pill.backgroundColor = [NSColor clearColor];
    _pill.hasShadow = NO;
    _pill.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces |
                               NSWindowCollectionBehaviorStationary |
                               NSWindowCollectionBehaviorFullScreenAuxiliary;
    _pill.ignoresMouseEvents = YES;
    _pill.hidesOnDeactivate = NO;

    _pillRoot = [[PillView alloc] initWithFrame:dummy];
    _pillRoot.wantsLayer = YES;
    _pill.contentView = _pillRoot;

    _pillDrop = makeShadowView([[NSColor blackColor] CGColor], 12, 0.28f, CGSizeMake(0, -8));
    [_pillRoot addSubview:_pillDrop];
    _pillGlow = makeShadowView([amber(1.0) CGColor], kGlowRestRadius, 0, CGSizeZero);
    [_pillRoot addSubview:_pillGlow];

    NSRect panel = NSMakeRect(kGlowPad, kGlowPad, kPillWidth, kChipH + 2 * kPillPadY);
    _pillContent = [[PillContentView alloc] initWithFrame:NSMakeRect(0, 0, panel.size.width, panel.size.height)];
    _pillContent.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
    if (@available(macOS 26.0, *)) {
        // Regular (not Clear) glass: it adapts light/dark to the backdrop,
        // which is what keeps the ink legible over white pages.
        NSGlassEffectView *glass = [[NSGlassEffectView alloc] initWithFrame:panel];
        glass.style = NSGlassEffectViewStyleRegular;
        glass.contentView = _pillContent;
        _pillGlass = glass;
    } else {
        NSVisualEffectView *fx = [[NSVisualEffectView alloc] initWithFrame:panel];
        fx.material = NSVisualEffectMaterialHUDWindow;
        fx.blendingMode = NSVisualEffectBlendingModeBehindWindow;
        fx.state = NSVisualEffectStateActive;
        fx.appearance = [NSAppearance appearanceNamed:NSAppearanceNameDarkAqua];
        fx.wantsLayer = YES;
        fx.layer.masksToBounds = YES;
        [fx addSubview:_pillContent];
        _pillGlass = fx;
    }
    [_pillRoot addSubview:_pillGlass];

    _pillTint = [[PillTintView alloc] initWithFrame:_pillContent.bounds];
    _pillTint.wantsLayer = YES;
    _pillTint.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
    _pillTint.alphaValue = 0;
    CAGradientLayer *wash = (CAGradientLayer *)_pillTint.layer;
    // Warm top to deep amber bottom, like light caught in tinted glass. The
    // colours are the ceiling's; rungs breathe the view's alpha below them.
    wash.colors = @[
        (id)[[NSColor colorWithRed:1.0 green:0.769 blue:0.361 alpha:0.62] CGColor],
        (id)[[NSColor colorWithRed:1.0 green:0.612 blue:0.141 alpha:0.46] CGColor],
        (id)[[NSColor colorWithRed:0.784 green:0.392 blue:0.0 alpha:0.40] CGColor],
    ];
    wash.locations = @[@0.0, @0.6, @1.0];
    wash.startPoint = CGPointMake(0.5, 1.0); // layer y grows upward: top first
    wash.endPoint = CGPointMake(0.5, 0.0);
    [_pillContent addSubview:_pillTint];

    _pillLabel = makePillLabel(_pillContent);
    _pillLabel.lineBreakMode = NSLineBreakByWordWrapping;
    [_pillLabel.cell setWraps:YES];
    [_pillLabel.cell setScrollable:NO];

    _pillChip = [[NSView alloc] initWithFrame:NSZeroRect];
    _pillChip.wantsLayer = YES;
    _pillChip.layer.cornerRadius = kChipH / 2;
    _pillChip.layer.borderWidth = 0.5;
    [_pillContent addSubview:_pillChip];
    _pillTimeLabel = makePillLabel(_pillChip);

    _pillOver = [[NSView alloc] initWithFrame:NSZeroRect];
    _pillOver.wantsLayer = YES;
    CALayer *over = _pillOver.layer;
    over.cornerRadius = kOverH / 2;
    over.backgroundColor = [overRed(1.0) CGColor];
    over.borderColor = [[NSColor colorWithWhite:1.0 alpha:0.9] CGColor];
    over.borderWidth = kOverRing;
    over.shadowColor = [overRed(1.0) CGColor];
    over.shadowRadius = 6;
    over.shadowOpacity = 0.55f;
    over.shadowOffset = CGSizeZero;
    [_pillChip addSubview:_pillOver];
    _pillOverLabel = makePillLabel(_pillOver);
}

// Applies the glass's current light/dark ink to text and chip. Colours never
// change metrics, so this is safe to call on an appearance flip without a
// relayout (and without moving the window).
static void stylePill(void) {
    if (!_pillLabel) return;
    BOOL dark = pillIsDark();
    _pillLabel.attributedStringValue = pillFocusAttr(dark);
    _pillTimeLabel.attributedStringValue = pillTimeAttr(dark);
    _pillChip.layer.backgroundColor = [[NSColor colorWithWhite:0 alpha:(dark ? 0.30 : 0.07)] CGColor];
    _pillChip.layer.borderColor = dark ? [[NSColor colorWithWhite:1 alpha:0.22] CGColor]
                                       : [[NSColor colorWithWhite:0 alpha:0.10] CGColor];
}

static void layoutPill(void) {
    if (!_pill) return;
    // Go owns the text/threshold rules; Cocoa keeps the clock and the minute
    // timer. Copy the C-owned runs before freeing them here.
    char *e = NULL, *b = NULL, *o = NULL;
    if (_sinceEpoch > 0) goHudFormatPillTime(nowSec() - _sinceEpoch, _budgetNanos, &e, &b, &o);
    [_pillElapsed release];
    [_pillBudget release];
    [_pillOverage release];
    _pillElapsed = [(e ? [NSString stringWithUTF8String:e] : @"") retain];
    _pillBudget = [(b ? [NSString stringWithUTF8String:b] : @"") retain];
    _pillOverage = [(o ? [NSString stringWithUTF8String:o] : @"") retain];
    free(e);
    free(b);
    free(o);

    BOOL dark = pillIsDark();
    NSAttributedString *focus = pillFocusAttr(dark);
    NSAttributedString *time = pillTimeAttr(dark);
    NSAttributedString *overText = pillOverAttr();
    BOOL hasChip = _pillElapsed.length > 0;
    BOOL hasOver = hasChip && _pillOverage.length > 0;

    CGFloat timeW = hasChip ? ceil(time.size.width) : 0;
    CGFloat overTextW = hasOver ? ceil(overText.size.width) : 0;
    CGFloat overW = hasOver ? overTextW + 2 * (kOverPadX + kOverRing) : 0;
    CGFloat chipW = 0;
    if (hasChip) {
        chipW = kChipPadX + timeW +
                (hasOver ? (kChipGap - kOverRing) + overW + (kChipOverEndPad - kOverRing) : kChipPadX);
    }
    CGFloat rightEdge = hasChip ? (kPillGap + chipW + kPillPadRight) : kPillPadLeft;
    CGFloat contentW = kPillWidth - kPillPadLeft - rightEdge;
    CGFloat textH = measureAttrHeight(focus, contentW);
    CGFloat panelH = MAX(kChipH + 2 * kPillPadY, textH + 2 * kPillTextPadY);
    CGFloat radius = MIN(panelH / 2, kPillMaxRadius);

    NSRect v = [NSScreen mainScreen].visibleFrame;
    NSRect pf;
    if ([_posPreset isEqualToString:@"top-right"]) {
        pf = NSMakeRect(NSMaxX(v) - kPillWidth - kSideMargin, NSMaxY(v) - panelH - kTopGap, kPillWidth, panelH);
    } else if ([_posPreset isEqualToString:@"top-left"]) {
        pf = NSMakeRect(NSMinX(v) + kSideMargin, NSMaxY(v) - panelH - kTopGap, kPillWidth, panelH);
    } else if ([_posPreset isEqualToString:@"custom"]) {
        CGFloat top = (_pillTop >= 0) ? _pillTop : (_posY + panelH);
        pf = NSMakeRect(_posX, top - panelH, kPillWidth, panelH);
        if (!rectOnAnyScreen(pf)) {
            pf = NSMakeRect(NSMidX(v) - kPillWidth / 2, NSMaxY(v) - panelH - kTopGap, kPillWidth, panelH);
        }
    } else { // top-center
        pf = NSMakeRect(NSMidX(v) - kPillWidth / 2, NSMaxY(v) - panelH - kTopGap, kPillWidth, panelH);
    }
    _pillTop = NSMaxY(pf);

    [_pill setFrame:NSInsetRect(pf, -kGlowPad, -kGlowPad) display:YES];
    NSRect glass = NSMakeRect(kGlowPad, kGlowPad, kPillWidth, panelH);
    _pillDrop.frame = _pillRoot.bounds;
    _pillGlow.frame = _pillRoot.bounds;
    shapeShadowView(_pillDrop, glass, radius, 0);
    shapeShadowView(_pillGlow, glass, radius, kGlowSpread);
    _pillGlass.frame = glass;
    if (@available(macOS 26.0, *)) {
        ((NSGlassEffectView *)_pillGlass).cornerRadius = radius;
    } else {
        _pillGlass.layer.cornerRadius = radius;
    }
    _pillContent.frame = NSMakeRect(0, 0, kPillWidth, panelH);
    _pillTint.frame = _pillContent.bounds;
    _pillTint.layer.cornerRadius = radius;

    _pillLabel.attributedStringValue = focus;
    _pillLabel.frame = NSMakeRect(kPillPadLeft, round((panelH - textH) / 2), contentW, textH);

    _pillChip.hidden = !hasChip;
    _pillOver.hidden = !hasOver;
    if (hasChip) {
        _pillChip.frame = NSMakeRect(kPillWidth - kPillPadRight - chipW, round((panelH - kChipH) / 2), chipW, kChipH);
        placeLabel(_pillTimeLabel, time, kChipPadX, kChipH / 2);
    }
    if (hasOver) {
        _pillOver.frame = NSMakeRect(kChipPadX + timeW + (kChipGap - kOverRing), (kChipH - kOverH) / 2, overW, kOverH);
        placeLabel(_pillOverLabel, overText, kOverRing + kOverPadX, kOverH / 2);
    }
    stylePill();
}

static void updateInteractivity(void) {
    if (!_pill) return;
    BOOL visible = _focusSet && !_paused;
    BOOL interactive = visible && (_pulsing || _idleOpacity > 0.0);
    _pill.ignoresMouseEvents = !interactive;
}

// The halo (shadow on _pillGlow's backing layer) and the wash (_pillTint's
// alpha) breathe in one animation group, so the completion handler that
// schedules the next half-cycle keeps the exact cadence of `period`.
static void pillBreathe(int gen, BOOL expand) {
    if (_pill == nil || _pulseGen != gen || !_pulsing) return;
    GlowSpec g = glowForRung(_rung);
    CALayer *glow = _pillGlow.layer;
    [NSAnimationContext runAnimationGroup:^(NSAnimationContext *ctx) {
        ctx.duration = g.period;
        ctx.allowsImplicitAnimation = YES;
        glow.shadowRadius = expand ? g.radiusMax : g.radiusMin;
        glow.shadowOpacity = expand ? g.opacityMax : g.opacityMin;
        _pillTint.animator.alphaValue = expand ? g.tintMax : g.tintMin;
    } completionHandler:^{
        dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(0.05 * NSEC_PER_SEC)),
                       dispatch_get_main_queue(), ^{
            pillBreathe(gen, !expand);
        });
    }];
}

static void endPulseNow(void) {
    _pulsing = NO;
    _pulseGen++;
    if (_pillGlow) {
        CALayer *glow = _pillGlow.layer;
        [NSAnimationContext runAnimationGroup:^(NSAnimationContext *ctx) {
            ctx.duration = 0.5;
            ctx.allowsImplicitAnimation = YES;
            glow.shadowRadius = kGlowRestRadius;
            glow.shadowOpacity = 0;
            _pillTint.animator.alphaValue = 0;
        }];
    }
    if (_pill && _focusSet && !_paused) {
        [NSAnimationContext runAnimationGroup:^(NSAnimationContext *ctx) {
            ctx.duration = 0.2;
            _pill.animator.alphaValue = _idleOpacity;
        } completionHandler:^{
            updateInteractivity();
        }];
    } else {
        updateInteractivity();
    }
}

static void killPulseSilent(void) {
    _pulsing = NO;
    _pulseGen++;
    if (_pillGlow) {
        // Drop any in-flight breathe so the hidden pill comes back at rest.
        [_pillGlow.layer removeAllAnimations];
        [_pillTint.layer removeAllAnimations];
        _pillGlow.layer.shadowRadius = kGlowRestRadius;
        _pillGlow.layer.shadowOpacity = 0;
        _pillTint.alphaValue = 0;
    }
}

static void refreshPillVisibility(void) {
    if (!_pill) return;
    if (_focusSet && !_paused) {
        layoutPill();
        if (!_pulsing) _pill.alphaValue = _idleOpacity;
        [_pill orderFrontRegardless];
    } else {
        killPulseSilent();
        [_pill orderOut:nil];
    }
    updateInteractivity();
}

static void pillAck(int kind) {
    // A click between reminders must not manufacture an acknowledgement or
    // delay cadence, even though the ambient pill always accepts drag input.
    if (!_pulsing) return;
    double latency = (_pulsing && _pulseShownAt > 0) ? (nowSec() - _pulseShownAt) : 0;
    int rung = _rung;
    unsigned long long reminderID = _pulseReminderID;
    endPulseNow();
    if (reminderID != 0) {
        goHudPassivePulseAck(kind, reminderID, latency);
    } else {
        goHudAck(kind, rung, latency, "");
    }
}

// --- takeover ---------------------------------------------------------------

@implementation KeyPanel
- (BOOL)canBecomeKeyWindow {
    return YES;
}
@end

@implementation KeyCatcherView
- (BOOL)acceptsFirstResponder {
    return YES;
}
- (void)keyDown:(NSEvent *)event {
    // Swallow everything; only the armed ack keys act. Not calling super also
    // suppresses the no-responder beep.
    if (!_tkArmed || _tkEditing) return;
    unsigned short kc = event.keyCode;
    if (kc == 36 || kc == 76) { // return / keypad-enter
        takeoverAck(kAckOnTask, nil);
        return;
    }
    NSString *chars = [event.charactersIgnoringModifiers lowercaseString];
    if ([chars isEqualToString:@"d"]) {
        takeoverAck(kAckDrifted, nil);
        return;
    }
    if ([chars isEqualToString:@"n"]) {
        beginRetype(NO);
        return;
    }
    if ([chars isEqualToString:@"f"]) {
        beginRetype(YES);
        return;
    }
}
- (BOOL)performKeyEquivalent:(NSEvent *)event {
    // Eat ⌘-combos too, except while the retype field needs ⌘V/⌘A.
    if (_tkEditing) return [super performKeyEquivalent:event];
    return YES;
}
@end

@implementation HudController
- (BOOL)control:(NSControl *)control textView:(NSTextView *)textView doCommandBySelector:(SEL)commandSelector {
    if (control != _tkField) return NO;
    BOOL doneMode = _tkDoneMode;
    if (commandSelector == @selector(insertNewline:)) {
        NSString *txt = [_tkField.stringValue stringByTrimmingCharactersInSet:
                            [NSCharacterSet whitespaceAndNewlineCharacterSet]];
        if (doneMode) {
            // ⏎ commits the completion; an empty field means nothing next.
            endRetype();
            takeoverAck(kAckDone, txt.length ? txt : nil);
        } else if (txt.length > 0) {
            endRetype();
            takeoverAck(kAckRefocus, txt);
        }
        return YES;
    }
    if (commandSelector == @selector(cancelOperation:)) {
        // ⎋ only backs out of the retype field — never out of the takeover,
        // and never completes anything: a mis-keyed F must be undoable.
        endRetype();
        return YES;
    }
    return NO;
}
- (void)windowDidResignKey:(NSNotification *)notification {
    // The takeover is the escalation: reclaim key (e.g. after ⌘-tab) until acked.
    if (notification.object == _tk && _tkVisible) {
        dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(0.05 * NSEC_PER_SEC)),
                       dispatch_get_main_queue(), ^{
            if (_tkVisible) [_tk makeKeyAndOrderFront:nil];
        });
    }
}
@end

static NSAttributedString *hintsWithPairs(NSArray *pairs) {
    NSMutableAttributedString *s = [[[NSMutableAttributedString alloc] init] autorelease];
    NSDictionary *keyAttrs = @{
        NSFontAttributeName: [NSFont systemFontOfSize:15 weight:NSFontWeightSemibold],
        NSForegroundColorAttributeName: cyan(0.95),
    };
    NSDictionary *labelAttrs = @{
        NSFontAttributeName: [NSFont systemFontOfSize:15 weight:NSFontWeightRegular],
        NSForegroundColorAttributeName: [NSColor colorWithWhite:1.0 alpha:0.60],
    };
    BOOL first = YES;
    for (NSArray *p in pairs) {
        if (!first) {
            [s appendAttributedString:[[[NSAttributedString alloc]
                initWithString:@"        " attributes:labelAttrs] autorelease]];
        }
        first = NO;
        [s appendAttributedString:[[[NSAttributedString alloc]
            initWithString:p[0] attributes:keyAttrs] autorelease]];
        [s appendAttributedString:[[[NSAttributedString alloc]
            initWithString:[@"  " stringByAppendingString:p[1]] attributes:labelAttrs] autorelease]];
    }
    NSMutableParagraphStyle *ps = [[[NSMutableParagraphStyle alloc] init] autorelease];
    ps.alignment = NSTextAlignmentCenter;
    [s addAttribute:NSParagraphStyleAttributeName value:ps range:NSMakeRange(0, s.length)];
    return s;
}

static NSAttributedString *armedHintString(void) {
    return hintsWithPairs(@[ @[@"⏎", @"still on it"], @[@"D", @"drifted"],
                             @[@"N", @"change focus"], @[@"F", @"done"] ]);
}

static NSAttributedString *editHintString(void) {
    return hintsWithPairs(@[ @[@"⏎", @"set new focus"], @[@"⎋", @"back"] ]);
}

static NSAttributedString *doneHintString(void) {
    return hintsWithPairs(@[ @[@"⏎", @"done — next focus optional"], @[@"⎋", @"back"] ]);
}

static void buildTakeover(void) {
    if (_tk) return;
    NSRect sf = [NSScreen mainScreen].frame;
    _tk = [[KeyPanel alloc] initWithContentRect:sf
                                      styleMask:NSWindowStyleMaskBorderless | NSWindowStyleMaskNonactivatingPanel
                                        backing:NSBackingStoreBuffered
                                          defer:NO];
    _tk.level = NSStatusWindowLevel + 2;
    _tk.opaque = NO;
    _tk.backgroundColor = [NSColor clearColor];
    _tk.hasShadow = NO;
    _tk.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces |
                             NSWindowCollectionBehaviorStationary |
                             NSWindowCollectionBehaviorFullScreenAuxiliary;
    _tk.hidesOnDeactivate = NO;
    _tk.releasedWhenClosed = NO;
    _tk.appearance = [NSAppearance appearanceNamed:NSAppearanceNameVibrantDark];
    _tk.delegate = _controller;

    _tkRoot = [[KeyCatcherView alloc] initWithFrame:NSMakeRect(0, 0, sf.size.width, sf.size.height)];
    _tkRoot.wantsLayer = YES;
    _tk.contentView = _tkRoot;

    _tkBlur = [[NSVisualEffectView alloc] initWithFrame:_tkRoot.bounds];
    _tkBlur.material = NSVisualEffectMaterialHUDWindow;
    _tkBlur.blendingMode = NSVisualEffectBlendingModeBehindWindow;
    _tkBlur.state = NSVisualEffectStateActive;
    _tkBlur.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
    [_tkRoot addSubview:_tkBlur];

    _tkDim = [[NSView alloc] initWithFrame:_tkRoot.bounds];
    _tkDim.wantsLayer = YES;
    _tkDim.layer.backgroundColor = [[NSColor colorWithWhite:0 alpha:0.42] CGColor];
    _tkDim.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
    [_tkRoot addSubview:_tkDim];

    _tkFocus = [[NSTextField labelWithString:@""] retain];
    _tkFocus.font = [NSFont systemFontOfSize:38 weight:NSFontWeightBold];
    _tkFocus.textColor = [NSColor whiteColor];
    _tkFocus.alignment = NSTextAlignmentCenter;
    _tkFocus.lineBreakMode = NSLineBreakByWordWrapping;
    [_tkFocus.cell setWraps:YES];
    [_tkFocus.cell setScrollable:NO];
    _tkFocus.wantsLayer = YES;
    _tkFocus.layer.shadowColor = [cyan(1.0) CGColor];
    _tkFocus.layer.shadowRadius = 18;
    _tkFocus.layer.shadowOpacity = 0.75;
    _tkFocus.layer.shadowOffset = CGSizeMake(0, 0);
    _tkFocus.layer.masksToBounds = NO;
    [_tkRoot addSubview:_tkFocus];

    _tkQuote = [[NSTextField labelWithString:@""] retain];
    _tkQuote.font = [NSFont systemFontOfSize:17];
    _tkQuote.textColor = [NSColor colorWithWhite:1.0 alpha:0.72];
    _tkQuote.alignment = NSTextAlignmentCenter;
    _tkQuote.lineBreakMode = NSLineBreakByWordWrapping;
    [_tkQuote.cell setWraps:YES];
    [_tkQuote.cell setScrollable:NO];
    [_tkRoot addSubview:_tkQuote];

    _tkMirror = [[NSTextField labelWithString:@""] retain];
    _tkMirror.font = [NSFont systemFontOfSize:13];
    _tkMirror.textColor = [NSColor colorWithWhite:1.0 alpha:0.45];
    _tkMirror.alignment = NSTextAlignmentCenter;
    [_tkRoot addSubview:_tkMirror];

    _tkHints = [[NSTextField labelWithString:@""] retain];
    _tkHints.alignment = NSTextAlignmentCenter;
    _tkHints.alphaValue = 0;
    [_tkRoot addSubview:_tkHints];

    _tkCircleHolder = [[NSView alloc] initWithFrame:NSMakeRect(0, 0, 120, 120)];
    _tkCircleHolder.wantsLayer = YES;
    _tkCircle = [[CAShapeLayer alloc] init];
    CGFloat r = 30;
    CGPathRef path = CGPathCreateWithEllipseInRect(CGRectMake(-r, -r, 2 * r, 2 * r), NULL);
    _tkCircle.path = path;
    CGPathRelease(path);
    _tkCircle.fillColor = [cyan(0.12) CGColor];
    _tkCircle.strokeColor = [cyan(0.9) CGColor];
    _tkCircle.lineWidth = 2;
    _tkCircle.shadowColor = [cyan(1.0) CGColor];
    _tkCircle.shadowRadius = 14;
    _tkCircle.shadowOpacity = 0.8;
    _tkCircle.shadowOffset = CGSizeZero;
    _tkCircle.position = CGPointMake(60, 60);
    [_tkCircleHolder.layer addSublayer:_tkCircle];
    [_tkRoot addSubview:_tkCircleHolder];

    _tkFieldBox = [[NSView alloc] initWithFrame:NSMakeRect(0, 0, 620, 52)];
    _tkFieldBox.wantsLayer = YES;
    _tkFieldBox.layer.backgroundColor = [[NSColor colorWithWhite:0.10 alpha:0.96] CGColor];
    _tkFieldBox.layer.cornerRadius = 10;
    _tkFieldBox.layer.borderColor = [cyan(0.7) CGColor];
    _tkFieldBox.layer.borderWidth = 1.5;
    _tkFieldBox.layer.shadowColor = [cyan(1.0) CGColor];
    _tkFieldBox.layer.shadowRadius = 12;
    _tkFieldBox.layer.shadowOpacity = 0.5;
    _tkFieldBox.layer.shadowOffset = CGSizeZero;
    _tkFieldBox.hidden = YES;
    [_tkRoot addSubview:_tkFieldBox];

    _tkField = [[NSTextField alloc] initWithFrame:NSMakeRect(16, 13, 620 - 32, 26)];
    _tkField.font = [NSFont systemFontOfSize:20 weight:NSFontWeightMedium];
    _tkField.textColor = [NSColor whiteColor];
    _tkField.bezeled = NO;
    _tkField.bordered = NO;
    _tkField.drawsBackground = NO;
    _tkField.focusRingType = NSFocusRingTypeNone;
    _tkField.alignment = NSTextAlignmentCenter;
    _tkField.delegate = _controller;
    [_tkFieldBox addSubview:_tkField];
}

static void layoutTakeover(void) {
    NSSize sz = _tkRoot.bounds.size;
    CGFloat W = sz.width, H = sz.height;

    CGFloat focusW = W * 0.78;
    CGFloat focusH = measureStringHeight(_tkFocus.stringValue, _tkFocus.font, focusW);
    _tkFocus.frame = NSMakeRect((W - focusW) / 2, H * 0.60 - focusH / 2, focusW, focusH);

    CGFloat quoteW = W * 0.60;
    CGFloat quoteH = measureStringHeight(_tkQuote.stringValue, _tkQuote.font, quoteW);
    _tkQuote.frame = NSMakeRect((W - quoteW) / 2, NSMinY(_tkFocus.frame) - 30 - quoteH, quoteW, quoteH);

    _tkCircleHolder.frame = NSMakeRect((W - 120) / 2, H * 0.36 - 60, 120, 120);
    _tkHints.frame = NSMakeRect((W - 700) / 2, H * 0.36 - 12, 700, 24);
    _tkFieldBox.frame = NSMakeRect((W - 620) / 2, H * 0.36 - 26, 620, 52);

    CGFloat mirrorW = W * 0.8;
    _tkMirror.frame = NSMakeRect((W - mirrorW) / 2, 44, mirrorW, 20);
}

static void startCircleBreathing(void) {
    [_tkCircle removeAllAnimations];
    CABasicAnimation *scale = [CABasicAnimation animationWithKeyPath:@"transform.scale"];
    scale.fromValue = @0.8;
    scale.toValue = @1.25;
    CABasicAnimation *glow = [CABasicAnimation animationWithKeyPath:@"shadowRadius"];
    glow.fromValue = @8.0;
    glow.toValue = @26.0;
    CAAnimationGroup *grp = [CAAnimationGroup animation];
    grp.animations = @[ scale, glow ];
    grp.duration = 1.4;
    grp.autoreverses = YES;
    grp.repeatCount = HUGE_VALF;
    grp.timingFunction = [CAMediaTimingFunction functionWithName:kCAMediaTimingFunctionEaseInEaseOut];
    [_tkCircle addAnimation:grp forKey:@"breathe"];
}

static void armTakeover(void) {
    _tkArmed = YES;
    [NSAnimationContext runAnimationGroup:^(NSAnimationContext *ctx) {
        ctx.duration = 0.5;
        _tkCircleHolder.animator.alphaValue = 0;
        _tkHints.animator.alphaValue = 1.0;
    } completionHandler:^{
        _tkCircleHolder.hidden = YES;
        [_tkCircle removeAllAnimations];
    }];
}

static void showTakeoverMain(NSString *focus, NSString *quote, NSString *mirror, int rung, double gate) {
    buildTakeover();
    _tkGen++;
    int gen = _tkGen;
    _tkVisible = YES;
    _tkArmed = NO;
    if (_tkEditing) endRetype();
    // The daemon owns the rung: 0 for routine check-ins, the escalated rung
    // in pulse mode (deriving it from the last pulse here went stale).
    _tkRung = rung;
    _tkShownAt = nowSec();

    [_tk setFrame:[NSScreen mainScreen].frame display:YES];
    _tkFocus.stringValue = focus ?: @"";
    _tkQuote.stringValue = quote.length ? [NSString stringWithFormat:@"“%@”", quote] : @"";
    _tkMirror.stringValue = mirror ?: @"";
    _tkHints.attributedStringValue = armedHintString();
    _tkHints.alphaValue = 0;
    _tkFieldBox.hidden = YES;
    layoutTakeover();

    BOOL useGate = gate > 0.05;
    _tkCircleHolder.hidden = !useGate;
    _tkCircleHolder.alphaValue = 1.0;

    _tk.alphaValue = 0;
    [_tk makeKeyAndOrderFront:nil];
    [_tk makeFirstResponder:_tkRoot];
    [NSAnimationContext runAnimationGroup:^(NSAnimationContext *ctx) {
        ctx.duration = 2.0;
        _tk.animator.alphaValue = 1.0;
    } completionHandler:^{
        // Gateless screens (routine check-ins) arm only once fully faded in:
        // keys armed on a near-invisible panel would let in-flight typing ack
        // a screen the user never saw.
        if (!useGate && _tkGen == gen && _tkVisible) armTakeover();
    }];

    if (useGate) {
        startCircleBreathing();
        dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(gate * NSEC_PER_SEC)),
                       dispatch_get_main_queue(), ^{
            if (_tkGen == gen && _tkVisible) armTakeover();
        });
    }
}

static void dismissTakeoverMain(void) {
    if (!_tkVisible) return;
    _tkVisible = NO;
    _tkArmed = NO;
    if (_tkEditing) endRetype();
    _tkGen++;
    int gen = _tkGen;
    [NSAnimationContext runAnimationGroup:^(NSAnimationContext *ctx) {
        ctx.duration = 0.4;
        _tk.animator.alphaValue = 0;
    } completionHandler:^{
        if (_tkGen == gen) {
            [_tk orderOut:nil];
            [_tkCircle removeAllAnimations];
        }
    }];
}

static void takeoverAck(int kind, NSString *newText) {
    if (!_tkVisible) return;
    double latency = nowSec() - _tkShownAt;
    int rung = _tkRung;
    dismissTakeoverMain();
    endPulseNow();
    goHudAck(kind, rung, latency, newText ? newText.UTF8String : "");
}

static void beginRetype(BOOL doneMode) {
    if (_tkEditing) return;
    _tkEditing = YES;
    _tkDoneMode = doneMode;
    _tkFieldBox.hidden = NO;
    _tkHints.attributedStringValue = doneMode ? doneHintString() : editHintString();
    // Drop the hints below the field; layoutTakeover restores them on next show.
    _tkHints.frame = NSMakeRect(_tkHints.frame.origin.x,
                                NSMinY(_tkFieldBox.frame) - 34,
                                _tkHints.frame.size.width, 24);
    // Done mode asks for the NEXT focus, so it starts empty; N edits the current one.
    _tkField.stringValue = doneMode ? @"" : (_tkFocus.stringValue ?: @"");
    _tkField.placeholderString = doneMode ? @"what's next?" : nil;
    [_tkField selectText:nil];
}

static void endRetype(void) {
    if (!_tkEditing) return;
    _tkEditing = NO;
    _tkDoneMode = NO;
    _tkFieldBox.hidden = YES;
    _tkHints.attributedStringValue = armedHintString();
    _tkHints.frame = NSMakeRect(_tkHints.frame.origin.x,
                                _tkRoot.bounds.size.height * 0.36 - 12,
                                _tkHints.frame.size.width, 24);
    [_tk makeFirstResponder:_tkRoot];
}

// --- timers / init ----------------------------------------------------------

static void startTimers(void) {
    _elapsedTimer = [[NSTimer scheduledTimerWithTimeInterval:60.0 repeats:YES block:^(NSTimer *t) {
        if (_focusSet && !_paused) layoutPill();
    }] retain];
}

// --- public API (called from hud_darwin.go) ---------------------------------

void hudInit(double idleOpacity, const char *posPreset, double posX, double posY,
             int pulseSeconds) {
    @autoreleasepool {
        // 0 is a valid idle opacity (pill invisible between pulses); only
        // out-of-range values fall back to the old default.
        _idleOpacity = (idleOpacity < 0.0 || idleOpacity > 1.0) ? 0.30 : idleOpacity;
        [_posPreset release];
        NSString *p = posPreset ? [NSString stringWithUTF8String:posPreset] : nil;
        _posPreset = [(p.length ? p : @"top-center") retain];
        _posX = posX;
        _posY = posY;
        _pulseSeconds = pulseSeconds > 0 ? pulseSeconds : 8;
    }
}

void hudRunApp(void) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        // No Dock icon when `focus daemon` runs from a terminal; the installed
        // bundle is LSUIElement anyway.
        [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
        _controller = [[HudController alloc] init];
        buildPill();
        startTimers();
    }
    [NSApp run];
}

void hudSetFocus(const char *text, double sinceEpoch, long long budgetNanos) {
    char *copy = strdup(text ? text : "");
    dispatch_async(dispatch_get_main_queue(), ^{
        [_focusText release];
        _focusText = [([NSString stringWithUTF8String:copy] ?: @"") retain];
        free(copy);
        _sinceEpoch = sinceEpoch;
        _budgetNanos = budgetNanos;
        _focusSet = YES;
        refreshPillVisibility();
    });
}

void hudApplyConfig(double idleOpacity, const char *posPreset, int pulseSeconds) {
    // Go frees its C string as soon as this call returns; the main-queue
    // block owns a copy until Cocoa can apply the update.
    char *copy = strdup(posPreset ? posPreset : "top-center");
    dispatch_async(dispatch_get_main_queue(), ^{
        _idleOpacity = idleOpacity;
        _pulseSeconds = pulseSeconds > 0 ? pulseSeconds : 8;
        NSString *preset = [NSString stringWithUTF8String:copy];
        // A drag may have reached Cocoa before its Go callback acquires the
        // daemon mutex. Honor local custom ownership too, so a queued reload
        // cannot snap the pill back to a preset during that handoff.
        if (![_posPreset isEqualToString:@"custom"] && ![preset isEqualToString:@"custom"]) {
            [_posPreset release];
            _posPreset = [preset retain];
        }
        free(copy);
        refreshPillVisibility();
    });
}

void hudClearFocus(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [_focusText release];
        _focusText = nil;
        _sinceEpoch = 0;
        _budgetNanos = 0;
        _focusSet = NO;
        refreshPillVisibility();
    });
}

void hudPulse(int rung, unsigned long long reminderID) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!_pill || !_focusSet || _paused) return;
        _rung = rung < 0 ? 0 : rung;
        _pulseReminderID = reminderID;
        _pulsing = YES;
        updateInteractivity();
        _pulseGen++;
        int gen = _pulseGen;
        _pulseShownAt = nowSec();
        GlowSpec g = glowForRung(_rung);
        _pillTint.layer.borderColor = [amberRim(g.borderAlpha) CGColor];
        _pillTint.layer.borderWidth = g.borderWidth;
        [NSAnimationContext runAnimationGroup:^(NSAnimationContext *ctx) {
            ctx.duration = 0.3;
            _pill.animator.alphaValue = 1.0;
        }];
        pillBreathe(gen, YES);
        // rung 0: PulseSeconds; rung 1: ~20s; rung 2+: glows until the next call.
        double dur = 0;
        if (_rung == 0) dur = (double)_pulseSeconds;
        else if (_rung == 1) dur = 20.0;
        if (dur > 0) {
            dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(dur * NSEC_PER_SEC)),
                           dispatch_get_main_queue(), ^{
                if (_pulseGen == gen) endPulseNow();
            });
        }
    });
}

void hudShowTakeover(const char *focusText, const char *quote,
                     const char *mirrorLine, int rung, double gateSeconds) {
    char *f = strdup(focusText ? focusText : "");
    char *q = strdup(quote ? quote : "");
    char *m = strdup(mirrorLine ? mirrorLine : "");
    dispatch_async(dispatch_get_main_queue(), ^{
        NSString *fs = [NSString stringWithUTF8String:f] ?: @"";
        NSString *qs = [NSString stringWithUTF8String:q] ?: @"";
        NSString *ms = [NSString stringWithUTF8String:m] ?: @"";
        free(f);
        free(q);
        free(m);
        showTakeoverMain(fs, qs, ms, rung, gateSeconds);
    });
}

void hudDismissTakeover(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        dismissTakeoverMain();
        endPulseNow();
    });
}

void hudStopPulse(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        // Cancel animation generations and their delayed completions before
        // returning to ambient opacity. Takeover ownership is independent.
        killPulseSilent();
        refreshPillVisibility();
    });
}

void hudSetPaused(int paused) {
    dispatch_async(dispatch_get_main_queue(), ^{
        _paused = paused != 0;
        refreshPillVisibility();
    });
}

// --- test hooks (hud/demo only) ----------------------------------------------

void hudTestKey(unsigned short keyCode, const char *chars) {
    char *copy = strdup(chars ? chars : "");
    dispatch_async(dispatch_get_main_queue(), ^{
        NSString *s = [NSString stringWithUTF8String:copy] ?: @"";
        free(copy);
        if (!_tk || !_tkVisible) return;
        NSEvent *e = [NSEvent keyEventWithType:NSEventTypeKeyDown
                                      location:NSZeroPoint
                                 modifierFlags:0
                                     timestamp:machTime()
                                  windowNumber:_tk.windowNumber
                                       context:nil
                                    characters:s
                   charactersIgnoringModifiers:s
                                     isARepeat:NO
                                       keyCode:keyCode];
        [_tk sendEvent:e];
    });
}

static NSEvent *pillMouseEvent(NSEventType type, NSPoint p, NSEventModifierFlags mods, int num) {
    return [NSEvent mouseEventWithType:type
                              location:p
                         modifierFlags:mods
                             timestamp:machTime()
                          windowNumber:_pill.windowNumber
                               context:nil
                           eventNumber:num
                            clickCount:1
                              pressure:1.0];
}

void hudTestPillClick(int optionHeld) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!_pill || !_pill.isVisible || _pill.ignoresMouseEvents) return;
        NSPoint p = NSMakePoint(_pill.frame.size.width / 2, _pill.frame.size.height / 2);
        NSEventModifierFlags mods = optionHeld ? NSEventModifierFlagOption : 0;
        [_pill sendEvent:pillMouseEvent(NSEventTypeLeftMouseDown, p, mods, 1)];
        [_pill sendEvent:pillMouseEvent(NSEventTypeLeftMouseUp, p, mods, 2)];
    });
}

void hudTestPillDrag(double dx, double dy) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!_pill || !_pill.isVisible || _pill.ignoresMouseEvents) return;
        NSPoint p = NSMakePoint(_pill.frame.size.width / 2, _pill.frame.size.height / 2);
        [_pill sendEvent:pillMouseEvent(NSEventTypeLeftMouseDown, p, 0, 3)];
        NSPoint moved = NSMakePoint(p.x + dx, p.y + dy);
        [_pill sendEvent:pillMouseEvent(NSEventTypeLeftMouseDragged, moved, 0, 4)];
        [_pill sendEvent:pillMouseEvent(NSEventTypeLeftMouseUp, moved, 0, 5)];
    });
}

double hudTestPillAlpha(void) {
    __block double alpha = -1;
    void (^readAlpha)(void) = ^{
        if (_pill) alpha = _pill.alphaValue;
    };
    if ([NSThread isMainThread]) readAlpha();
    else dispatch_sync(dispatch_get_main_queue(), readAlpha);
    return alpha;
}

static void snapshotView(NSView *view, NSString *path) {
    NSSize sz = view.bounds.size;
    if (sz.width < 1 || sz.height < 1) return;
    NSBitmapImageRep *rep = [[NSBitmapImageRep alloc]
        initWithBitmapDataPlanes:NULL
                      pixelsWide:(NSInteger)sz.width
                      pixelsHigh:(NSInteger)sz.height
                   bitsPerSample:8
                 samplesPerPixel:4
                        hasAlpha:YES
                        isPlanar:NO
                  colorSpaceName:NSCalibratedRGBColorSpace
                     bytesPerRow:0
                    bitsPerPixel:0];
    NSGraphicsContext *ctx = [NSGraphicsContext graphicsContextWithBitmapImageRep:rep];
    [NSGraphicsContext saveGraphicsState];
    [NSGraphicsContext setCurrentContext:ctx];
    CGContextRef cg = ctx.CGContext;
    // Neutral dark backdrop so the glow reads in the png.
    CGContextSetRGBFillColor(cg, 0.13, 0.10, 0.25, 1.0);
    CGContextFillRect(cg, CGRectMake(0, 0, sz.width, sz.height));
    [view.layer renderInContext:cg];
    [NSGraphicsContext restoreGraphicsState];
    NSData *png = [rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
    [png writeToFile:path atomically:YES];
    [rep release];
}

// CGWindowListCreateImage is obsoleted in the macOS 15+ SDK headers but still
// exported at runtime, so it is looked up dynamically. Unlike screencapture
// from an agent shell, it may capture this process's own windows without
// Screen Recording permission: the composite shows the real glass, blur and
// window alpha over our backdrop window (other apps' windows are omitted).
typedef CGImageRef (*WindowListCreateImageFn)(CGRect, uint32_t, uint32_t, uint32_t);
enum { kWindowListOnScreenOnly = 1 };

static NSPanel *_testBackdrop = nil;

void hudTestBackdrop(const char *imagePath) {
    char *copy = strdup(imagePath ? imagePath : "");
    dispatch_async(dispatch_get_main_queue(), ^{
        NSString *path = [NSString stringWithUTF8String:copy] ?: @"";
        free(copy);
        NSImage *img = [[[NSImage alloc] initWithContentsOfFile:path] autorelease];
        if (!_pill || !img) {
            fprintf(stderr, "[hud] backdrop: cannot load %s\n", path.UTF8String);
            return;
        }
        if (!_testBackdrop) {
            _testBackdrop = [[NSPanel alloc] initWithContentRect:NSZeroRect
                                                       styleMask:NSWindowStyleMaskBorderless | NSWindowStyleMaskNonactivatingPanel
                                                         backing:NSBackingStoreBuffered
                                                           defer:NO];
            // Same level as the pill, ordered just under it, so another
            // running focus pill (also at this level) can't slip between the
            // backdrop and the glass and skew its light/dark sampling.
            _testBackdrop.level = NSStatusWindowLevel + 1;
            _testBackdrop.ignoresMouseEvents = YES;
            _testBackdrop.hasShadow = NO;
            _testBackdrop.contentView.wantsLayer = YES;
            _testBackdrop.contentView.layer.contentsGravity = kCAGravityResizeAspectFill;
        }
        [_testBackdrop setFrame:_pill.frame display:NO];
        _testBackdrop.contentView.layer.contents = img;
        [_testBackdrop orderFrontRegardless];
        [_pill orderFrontRegardless];
    });
}

// Writes the window server's composite of the window's screen rect. Returns NO
// when the capture API is unavailable or refuses.
static BOOL snapshotWindow(NSWindow *win, NSString *path) {
    WindowListCreateImageFn create =
        (WindowListCreateImageFn)dlsym(RTLD_DEFAULT, "CGWindowListCreateImage");
    if (!create) return NO;
    // Cocoa's origin is the primary screen's bottom-left; CG's is its top-left.
    NSRect f = win.frame;
    CGFloat primaryH = [NSScreen screens][0].frame.size.height;
    CGRect cg = CGRectMake(f.origin.x, primaryH - NSMaxY(f), f.size.width, f.size.height);
    CGImageRef img = create(cg, kWindowListOnScreenOnly, 0, 0);
    if (!img) return NO;
    NSBitmapImageRep *rep = [[NSBitmapImageRep alloc] initWithCGImage:img];
    BOOL ok = [[rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}]
                  writeToFile:path atomically:YES];
    [rep release];
    CGImageRelease(img);
    return ok;
}

void hudTestSnapshot(const char *pillPath, const char *takeoverPath) {
    char *p = strdup(pillPath ? pillPath : "");
    char *t = strdup(takeoverPath ? takeoverPath : "");
    dispatch_async(dispatch_get_main_queue(), ^{
        NSString *ps = [NSString stringWithUTF8String:p] ?: @"";
        NSString *ts = [NSString stringWithUTF8String:t] ?: @"";
        free(p);
        free(t);
        // The pill is mostly glass, which a layer render can't draw, so it
        // prefers the real composite and falls back to the layer render.
        if (ps.length && _pill && _pill.isVisible && !snapshotWindow(_pill, ps)) {
            snapshotView(_pillRoot, ps);
        }
        if (ts.length && _tk && _tkVisible) snapshotView(_tkRoot, ts);
    });
}
