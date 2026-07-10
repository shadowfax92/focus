// focus HUD: ambient glow pill + pulse ladder + full-screen ack takeover.
// Visual language lifted from mac-notify's overlay (dark panel, cyan glow,
// generation-guarded breathing loops). Memory model is MRC (no ARC flags in
// the cgo build): long-lived views are created once and retained forever;
// per-show strings are strdup'd outside the main-queue hop and freed inside.
#import <AppKit/AppKit.h>
#import <QuartzCore/QuartzCore.h>
#include <stdlib.h>
#include <string.h>
#include "hud_darwin.h"

// Implemented in hud_darwin.go (//export).
extern void goHudAck(int kind, int rung, double latencySeconds, const char *newText);
extern void goHudMoved(double x, double y);

// Mirrors hud.AckKind iota order.
enum { kAckOnTask = 0, kAckDrifted = 1, kAckRefocus = 2 };

static const CGFloat kPillWidth = 460;
static const CGFloat kPillPadX = 18;
static const CGFloat kPillPadY = 13;
// Transparent margin around the visual panel so the layer shadow (the glow)
// has room to render instead of clipping at the window edge.
static const CGFloat kGlowPad = 44;
static const CGFloat kTopGap = 8;
static const CGFloat kSideMargin = 16;

// --- forward declarations -------------------------------------------------

static void layoutPill(void);
static void refreshPillVisibility(void);
static void updateInteractivity(void);
static void pillBreathe(int gen, BOOL expand);
static void endPulseNow(void);
static void killPulseSilent(void);
static void pillAck(int kind);
static void layoutTakeover(void);
static void showTakeoverMain(NSString *focus, NSString *quote, NSString *mirror, double gate);
static void dismissTakeoverMain(void);
static void armTakeover(void);
static void takeoverAck(int kind, NSString *newText);
static void beginRetype(void);
static void endRetype(void);
static void startCircleBreathing(void);
static NSAttributedString *armedHintString(void);
static NSAttributedString *editHintString(void);

static NSColor *cyan(CGFloat alpha) {
    return [NSColor colorWithRed:0 green:0.85 blue:1.0 alpha:alpha];
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

static NSPanel *_pill = nil;
static PillView *_pillRoot = nil;
static NSView *_pillPanelView = nil;
static NSTextField *_pillLabel = nil;
static NSString *_focusText = nil;
static double _sinceEpoch = 0;
static BOOL _focusSet = NO;
static BOOL _paused = NO;
static BOOL _optHeld = NO;
static BOOL _pulsing = NO;
static int _rung = 0;
static int _pulseGen = 0;
static double _pulseShownAt = 0;
// Screen y of the pill panel's top edge; text growth extends downward from it.
static CGFloat _pillTop = -1;
static NSTimer *_optTimer = nil;
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
static int _tkGen = 0;
static double _tkShownAt = 0;
static int _tkRung = 0;

// --- pill -------------------------------------------------------------------

@implementation PillView
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

typedef struct {
    CGFloat radiusMin, radiusMax;
    float opacityMin, opacityMax;
    CGFloat borderWidth;
    CGFloat borderAlpha;
    double period;
} GlowSpec;

static GlowSpec glowForRung(int rung) {
    if (rung <= 0) return (GlowSpec){8, 20, 0.40f, 0.90f, 1.5, 0.60, 1.0};
    if (rung == 1) return (GlowSpec){12, 30, 0.60f, 1.00f, 2.0, 0.85, 0.65};
    return (GlowSpec){22, 36, 0.85f, 1.00f, 2.5, 1.00, 0.50};
}

static NSString *elapsedSuffix(void) {
    if (_sinceEpoch <= 0) return nil;
    long mins = (long)((nowSec() - _sinceEpoch) / 60.0);
    if (mins < 1) return nil;
    if (mins < 60) return [NSString stringWithFormat:@"· %ldm", mins];
    long h = mins / 60, m = mins % 60;
    if (m == 0) return [NSString stringWithFormat:@"· %ldh", h];
    return [NSString stringWithFormat:@"· %ldh %ldm", h, m];
}

static NSAttributedString *pillAttr(void) {
    NSMutableAttributedString *s = [[[NSMutableAttributedString alloc] init] autorelease];
    [s appendAttributedString:[[[NSAttributedString alloc]
        initWithString:(_focusText ?: @"")
            attributes:@{
                NSFontAttributeName: [NSFont systemFontOfSize:15 weight:NSFontWeightSemibold],
                NSForegroundColorAttributeName: [NSColor colorWithWhite:1.0 alpha:0.95],
            }] autorelease]];
    NSString *elapsed = elapsedSuffix();
    if (elapsed) {
        [s appendAttributedString:[[[NSAttributedString alloc]
            initWithString:[@"  " stringByAppendingString:elapsed]
                attributes:@{
                    NSFontAttributeName: [NSFont systemFontOfSize:15 weight:NSFontWeightRegular],
                    NSForegroundColorAttributeName: [NSColor colorWithWhite:1.0 alpha:0.55],
                }] autorelease]];
    }
    return s;
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

    _pillPanelView = [[NSView alloc] initWithFrame:NSMakeRect(kGlowPad, kGlowPad, kPillWidth, 20)];
    _pillPanelView.wantsLayer = YES;
    CALayer *layer = _pillPanelView.layer;
    layer.cornerRadius = 12;
    layer.backgroundColor = [[NSColor colorWithWhite:0.08 alpha:0.95] CGColor];
    layer.borderColor = [cyan(0.6) CGColor];
    layer.borderWidth = 1.5;
    layer.shadowColor = [cyan(1.0) CGColor];
    layer.shadowRadius = 8;
    layer.shadowOpacity = 0.4;
    layer.shadowOffset = CGSizeMake(0, 0);
    [_pillRoot addSubview:_pillPanelView];

    _pillLabel = [[NSTextField labelWithString:@""] retain];
    _pillLabel.lineBreakMode = NSLineBreakByWordWrapping;
    [_pillLabel.cell setWraps:YES];
    [_pillLabel.cell setScrollable:NO];
    [_pillPanelView addSubview:_pillLabel];
}

static void layoutPill(void) {
    if (!_pill) return;
    NSAttributedString *attr = pillAttr();
    CGFloat contentW = kPillWidth - 2 * kPillPadX;
    CGFloat textH = measureAttrHeight(attr, contentW);
    CGFloat panelH = textH + 2 * kPillPadY;

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
    _pillPanelView.frame = NSMakeRect(kGlowPad, kGlowPad, kPillWidth, panelH);
    _pillLabel.attributedStringValue = attr;
    _pillLabel.frame = NSMakeRect(kPillPadX, kPillPadY, contentW, textH);
}

static void updateInteractivity(void) {
    if (!_pill) return;
    BOOL visible = _focusSet && !_paused;
    BOOL interactive = visible && (_pulsing || _optHeld);
    _pill.ignoresMouseEvents = !interactive;
    if (!visible || _pulsing) return; // the pulse owns alpha while active
    CGFloat target = interactive ? 0.95 : _idleOpacity;
    if (fabs(_pill.alphaValue - target) < 0.01) return;
    [NSAnimationContext runAnimationGroup:^(NSAnimationContext *ctx) {
        ctx.duration = 0.2;
        _pill.animator.alphaValue = target;
    }];
}

static void pillBreathe(int gen, BOOL expand) {
    if (_pill == nil || _pulseGen != gen || !_pulsing) return;
    GlowSpec g = glowForRung(_rung);
    CALayer *layer = _pillPanelView.layer;
    [NSAnimationContext runAnimationGroup:^(NSAnimationContext *ctx) {
        ctx.duration = g.period;
        ctx.allowsImplicitAnimation = YES;
        layer.shadowRadius = expand ? g.radiusMax : g.radiusMin;
        layer.shadowOpacity = expand ? g.opacityMax : g.opacityMin;
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
    if (_pillPanelView) {
        CALayer *layer = _pillPanelView.layer;
        layer.borderWidth = 1.5;
        layer.borderColor = [cyan(0.6) CGColor];
        [NSAnimationContext runAnimationGroup:^(NSAnimationContext *ctx) {
            ctx.duration = 0.5;
            ctx.allowsImplicitAnimation = YES;
            layer.shadowRadius = 8;
            layer.shadowOpacity = 0.4;
        }];
    }
    updateInteractivity();
}

static void killPulseSilent(void) {
    _pulsing = NO;
    _pulseGen++;
    if (_pillPanelView) {
        CALayer *layer = _pillPanelView.layer;
        layer.borderWidth = 1.5;
        layer.borderColor = [cyan(0.6) CGColor];
        layer.shadowRadius = 8;
        layer.shadowOpacity = 0.4;
    }
}

static void refreshPillVisibility(void) {
    if (!_pill) return;
    if (_focusSet && !_paused) {
        layoutPill();
        if (!_pulsing) _pill.alphaValue = _optHeld ? 0.95 : _idleOpacity;
        [_pill orderFrontRegardless];
    } else {
        killPulseSilent();
        [_pill orderOut:nil];
    }
    updateInteractivity();
}

static void pillAck(int kind) {
    double latency = (_pulsing && _pulseShownAt > 0) ? (nowSec() - _pulseShownAt) : 0;
    int rung = _rung;
    endPulseNow();
    goHudAck(kind, rung, latency, "");
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
        beginRetype();
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
    if (commandSelector == @selector(insertNewline:)) {
        NSString *txt = [_tkField.stringValue stringByTrimmingCharactersInSet:
                            [NSCharacterSet whitespaceAndNewlineCharacterSet]];
        if (txt.length > 0) {
            endRetype();
            takeoverAck(kAckRefocus, txt);
        }
        return YES;
    }
    if (commandSelector == @selector(cancelOperation:)) {
        // ⎋ only backs out of the retype field — never out of the takeover.
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
    return hintsWithPairs(@[ @[@"⏎", @"on task"], @[@"D", @"drifted"], @[@"N", @"new focus"] ]);
}

static NSAttributedString *editHintString(void) {
    return hintsWithPairs(@[ @[@"⏎", @"set new focus"], @[@"⎋", @"back"] ]);
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

static void showTakeoverMain(NSString *focus, NSString *quote, NSString *mirror, double gate) {
    buildTakeover();
    _tkGen++;
    int gen = _tkGen;
    _tkVisible = YES;
    _tkArmed = NO;
    if (_tkEditing) endRetype();
    // The takeover is the next rung above whatever last pulsed.
    _tkRung = _rung + 1;
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
    }];

    if (useGate) {
        startCircleBreathing();
        dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(gate * NSEC_PER_SEC)),
                       dispatch_get_main_queue(), ^{
            if (_tkGen == gen && _tkVisible) armTakeover();
        });
    } else {
        armTakeover();
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

static void beginRetype(void) {
    if (_tkEditing) return;
    _tkEditing = YES;
    _tkFieldBox.hidden = NO;
    _tkHints.attributedStringValue = editHintString();
    // Drop the hints below the field; layoutTakeover restores them on next show.
    _tkHints.frame = NSMakeRect(_tkHints.frame.origin.x,
                                NSMinY(_tkFieldBox.frame) - 34,
                                _tkHints.frame.size.width, 24);
    _tkField.stringValue = _tkFocus.stringValue ?: @"";
    [_tkField selectText:nil];
}

static void endRetype(void) {
    if (!_tkEditing) return;
    _tkEditing = NO;
    _tkFieldBox.hidden = YES;
    _tkHints.attributedStringValue = armedHintString();
    _tkHints.frame = NSMakeRect(_tkHints.frame.origin.x,
                                _tkRoot.bounds.size.height * 0.36 - 12,
                                _tkHints.frame.size.width, 24);
    [_tk makeFirstResponder:_tkRoot];
}

// --- timers / init ----------------------------------------------------------

static void startTimers(void) {
    // ⌥ detection: +[NSEvent modifierFlags] reads current hardware state with
    // no permissions; a global event monitor would prompt for Input Monitoring.
    _optTimer = [[NSTimer scheduledTimerWithTimeInterval:0.15 repeats:YES block:^(NSTimer *t) {
        BOOL held = ([NSEvent modifierFlags] & NSEventModifierFlagOption) != 0;
        if (held != _optHeld) {
            _optHeld = held;
            updateInteractivity();
        }
    }] retain];
    _elapsedTimer = [[NSTimer scheduledTimerWithTimeInterval:60.0 repeats:YES block:^(NSTimer *t) {
        if (_focusSet && !_paused) layoutPill();
    }] retain];
}

// --- public API (called from hud_darwin.go) ---------------------------------

void hudInit(double idleOpacity, const char *posPreset, double posX, double posY,
             int pulseSeconds) {
    @autoreleasepool {
        _idleOpacity = (idleOpacity <= 0.0 || idleOpacity > 1.0) ? 0.30 : idleOpacity;
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

void hudSetFocus(const char *text, double sinceEpoch) {
    char *copy = strdup(text ? text : "");
    dispatch_async(dispatch_get_main_queue(), ^{
        [_focusText release];
        _focusText = [([NSString stringWithUTF8String:copy] ?: @"") retain];
        free(copy);
        _sinceEpoch = sinceEpoch;
        _focusSet = YES;
        refreshPillVisibility();
    });
}

void hudClearFocus(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [_focusText release];
        _focusText = nil;
        _sinceEpoch = 0;
        _focusSet = NO;
        refreshPillVisibility();
    });
}

void hudPulse(int rung) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!_pill || !_focusSet || _paused) return;
        _rung = rung < 0 ? 0 : rung;
        _pulsing = YES;
        _pulseGen++;
        int gen = _pulseGen;
        _pulseShownAt = nowSec();
        GlowSpec g = glowForRung(_rung);
        _pillPanelView.layer.borderColor = [cyan(g.borderAlpha) CGColor];
        _pillPanelView.layer.borderWidth = g.borderWidth;
        updateInteractivity();
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
                     const char *mirrorLine, double gateSeconds) {
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
        showTakeoverMain(fs, qs, ms, gateSeconds);
    });
}

void hudDismissTakeover(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        dismissTakeoverMain();
        endPulseNow();
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
        if (!_pill || !_pill.isVisible) return;
        NSPoint p = NSMakePoint(_pill.frame.size.width / 2, _pill.frame.size.height / 2);
        NSEventModifierFlags mods = optionHeld ? NSEventModifierFlagOption : 0;
        [_pill sendEvent:pillMouseEvent(NSEventTypeLeftMouseDown, p, mods, 1)];
        [_pill sendEvent:pillMouseEvent(NSEventTypeLeftMouseUp, p, mods, 2)];
    });
}

void hudTestPillDrag(double dx, double dy) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!_pill || !_pill.isVisible) return;
        NSPoint p = NSMakePoint(_pill.frame.size.width / 2, _pill.frame.size.height / 2);
        [_pill sendEvent:pillMouseEvent(NSEventTypeLeftMouseDown, p, 0, 3)];
        NSPoint moved = NSMakePoint(p.x + dx, p.y + dy);
        [_pill sendEvent:pillMouseEvent(NSEventTypeLeftMouseDragged, moved, 0, 4)];
        [_pill sendEvent:pillMouseEvent(NSEventTypeLeftMouseUp, moved, 0, 5)];
    });
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

void hudTestSnapshot(const char *pillPath, const char *takeoverPath) {
    char *p = strdup(pillPath ? pillPath : "");
    char *t = strdup(takeoverPath ? takeoverPath : "");
    dispatch_async(dispatch_get_main_queue(), ^{
        NSString *ps = [NSString stringWithUTF8String:p] ?: @"";
        NSString *ts = [NSString stringWithUTF8String:t] ?: @"";
        free(p);
        free(t);
        if (ps.length && _pill && _pill.isVisible) snapshotView(_pillRoot, ps);
        if (ts.length && _tk && _tkVisible) snapshotView(_tkRoot, ts);
    });
}
