# focus — design

One ambient focus pill keeps the main thing visible; every `interval` a
full-screen check-in asks whether you're still on it — with honest
day-over-day distraction stats. The v1 pulse escalation ladder survives as an
opt-in cadence.

Deliberately **separate from mac-notify** (that stays a pure notification queue).
This tool is a persistent, stateful HUD with its own daemon, socket, and app bundle.

## Reminder styles — `reminder_style`

- **`fullscreen` (default).** The ambient pill stays visible at
  `idle_opacity`. Every `interval` (15m/30m — user's call) the full-screen
  check-in appears directly: no pulse rungs, no `escalate_after` gating. While
  one is up, further ticks are absorbed (never a second screen, never rung
  growth — an idle stretch spent staring at one doesn't re-fire it either).
  Routine check-ins skip the breathing circle
  (`Gate: 0`): a gate 4×/hour would be pure friction — `breathing_gate_seconds`
  applies to pulse-mode escalations only. The keys still arm only once the 2s
  fade-in completes, so in-flight typing can never ack a screen that isn't
  visible yet. Setting a focus does **not** fire an instant screen; the first
  check-in comes a full interval later.
- **`pulse`.** The v1 cadence, unchanged: the same ambient pill shows glow
  pulses climbing rungs each unacked tick, with a takeover after
  `escalate_after` ignored pulses.

Both styles share the ambient pill, idle guard, pause/resume, ack vocabulary,
and takeover screen itself.

## UX spec

### Pill (both styles)

- Dark rounded floating panel. Visual language lifted from
  `/Users/shadowfax/code/clis/mac-notify/menubar/notify_darwin.m`
  (white-0.08 background, cyan glow border, corner radius 12) but wider: ~460pt,
  height auto-fit to text.
- Always on top (`NSStatusWindowLevel + 1`), joins all Spaces, full-screen
  auxiliary, stationary.
- Content: focus text + dim elapsed suffix `· 47m` (ticks once a minute).
- Idle: opacity = `idle_opacity` (default 0.30), fully click-through
  (`ignoresMouseEvents = YES`).
- Interactive only: while pulsing, or while ⌥ is held (poll `+[NSEvent
  modifierFlags]` on a timer — no permissions needed; global event monitors
  would prompt for Input Monitoring).
- Draggable when interactive; on drag end the new origin is reported to the
  daemon and persisted. Config also supports presets: `top-center` (default),
  `top-right`, `top-left`.
- Hidden entirely when paused or when no focus is set.

### Pulse ladder (pulse style only)

- Every `interval` (default 15m) the daemon fires a pulse at the current rung:
  - rung 0: breathe to full opacity + glow for `pulse_seconds` (default 8s)
  - rung 1: ~20s, brighter glow
  - rung 2+: constant glow until acked or the next tick
- The rung increments each tick that passes with no ack since the previous
  pulse; any ack resets rung to 0.
- After `escalate_after` (default 2) consecutive unacked pulses, the next
  reminder is the **takeover** instead of a pulse.
- During an active pulse, clicking the pill acknowledges it: **left-click =
  on_task**, **⌥-click = drifted**. Between pulses, holding ⌥ makes the ambient
  pill draggable; a click without a drag is ignored.
- Idle guard (both styles): if the user has been idle longer than
  `idle_pause_minutes` (default 5), ticks are skipped entirely — no rung
  growth, no screens at an empty desk. On return from an idle stretch ≥
  `idle_pause_minutes`, fire an immediate welcome-back reminder (rung-0 pulse
  in pulse style, a check-in in fullscreen style) and log `idle_return`.
- `pause` hides the pill and stops ticks until `resume` (or the duration lapses).

### Takeover (the check-in screen; top of the ladder in pulse style)

- Full-screen panel on the main screen. `NSVisualEffectView` blur — the work
  visibly dissolves behind it — fading in over ~2s.
- Centered column: glowing focus text (large), a random quote from config
  below it, and a dim mirror footer computed by the daemon:
  `3rd check-in today · 43m on task · yesterday: 2 distractions` (fullscreen)
  or `2nd escalation today · yesterday: 5 · 43m on task` (pulse escalation).
- Breathing gate: a breathing-circle animation for `breathing_gate_seconds`
  (default 3, 0 = off) before the ack keys arm. Key hints appear after the
  gate. Routine check-ins always pass gate 0, skip the breathing circle, and
  arm after the 2s fade-in completes.
- Keyboard-first; the panel becomes key and **swallows all keystrokes**:
  - `Enter` → on_task ("still on it")
  - `D` → drifted
  - `N` → inline text field pre-filled with the current focus → refocus ack
    (the new text rides along; the daemon sets it as the current focus);
    `⎋` backs out to the armed keys
  - `F` → done. The same inline field opens empty ("what's next?"):
    - `Enter` with text → done ack carrying the next focus — the daemon logs
      completion and starts the new focus in one stroke
    - `Enter` on the empty field → done with nothing next: focus cleared,
      screen closes, no reminders until the next `focus set`
    - `⎋` → backs out to the armed keys, completing nothing (a mis-keyed F
      must be undoable; ⎋ is never destructive)
- No mouse required; `⎋` never dismisses the screen itself. On ack: fade out,
  restore whatever was key before.
- The daemon passes the rung explicitly (`TakeoverContent.Rung`): 0 for
  check-ins, the escalated rung in pulse style. Acks echo it back.
- Never shown while the idle guard is active.

## Acks and stats

Event log: append-only JSONL at `~/.local/share/focus/events.jsonl`:

```json
{"ts":"2026-07-09T14:05:00Z","type":"checkin"}
{"ts":"...","type":"ack","kind":"drifted","rung":0,"latency_s":4.2}
{"ts":"...","type":"set","text":"ship onboarding PR"}
```

`type`: `set | checkin | pulse | ack | escalation | done | pause | resume | idle_return`
`kind` (acks): `on_task | drifted | refocus | done`

**Distraction (the metric) = a `drifted` ack OR an `escalation` shown.**
Routine `checkin` events are reminders, not distractions — in fullscreen mode
the metric is drifted acks only, so every-15m screens never inflate the count.
A done ack from the screen logs `ack` (kind `done`, with latency stamped from
when the screen appeared) → `done` → `set` when a next focus was typed.
Everything is derived at read time from the JSONL; no aggregate state.

- `focus stats` — today vs yesterday: distractions, check-ins, pulses, acks,
  avg ack latency, DoD %. **Down is good**: fewer distractions renders green.
- `focus stats --days N` — vertical bar chart of distractions/day, exactly the
  gh-stats look (`/Users/shadowfax/code/clis/gh-stats/render/render.go` —
  `VerticalBars`, `Sparkline`, `FormatPctInt`, fatih/color).
- `focus stats weeks` — WoW comparison rows + per-week sparklines + streak line
  ("3 days improving").
- `--json` on all stats views.

## CLI

```
focus set "ship the onboarding PR"   # set/replace focus; pill appears
focus done                           # logs done, clears focus, hides pill
focus clear                          # alias of done
focus status                         # current focus + elapsed + rung + paused state
focus pause 45m                      # meeting mode
focus resume
focus ack [--drifted]                # ack from the CLI (default on_task)
focus stats [--days N | weeks] [--json]
focus quotes add "..." | list | rm <n>
focus config                         # print resolved config
focus install | uninstall            # app bundle + launchd agent
focus daemon                         # run daemon in foreground (dev)
```

## Config — `~/.config/focus/config.yaml` (defaults shown)

```yaml
reminder_style: fullscreen   # fullscreen | pulse
interval: 15m
pulse_seconds: 8             # pulse style only
escalate_after: 2            # pulse style only
breathing_gate_seconds: 3    # pulse-style escalations; check-ins arm on fade-in
idle_opacity: 0.30           # ambient pill in both styles; 0 is honored
idle_pause_minutes: 5
position:
  preset: top-center   # top-center | top-right | top-left | custom
  x: 0                 # used when preset: custom (saved on drag)
  y: 0
quotes:
  - "The main thing is to keep the main thing the main thing."
```

Runtime state (survives daemon restart): `~/.local/state/focus/current.json` —
current focus text, set-at timestamp, paused-until, saved custom position.

## IPC

Unix socket `~/.focus.sock`, JSON request/response, one connection per command
(same pattern as `/Users/shadowfax/code/clis/mac-notify/ipc/`). Verbs:
`set, done, status, pause, resume, ack, ping`. The CLI prints a helpful error
(`focus install` / `focus daemon`) when the daemon is down.

## Architecture

**Policy in Go, pixels in Objective-C.** The daemon decides *when* anything
happens; `hud` only draws and reports input. (The v1 two-lane parallel build
and its frozen `hud/hud.go` contract are history — the API changes with the
code now.)

```
main.go            tiny; init() locks the main OS thread (Cocoa needs it)
cmd/               cobra CLI
ipc/               socket client/server/protocol
config/            yaml load/save, defaults
store/             events.jsonl append + stats derivation
render/            terminal charts (gh-stats style)
daemon/            tick scheduler, reminder state machine,
                   idle detection (one tiny cgo file for
                   CGEventSourceSecondsSinceLastEventType)
hud/               Go API + objc implementation
                   + hud/demo visual harness
Makefile, plist    build, Focus.app (LSUIElement), launchd
```

- The daemon drives pill focus state in both reminder styles. In fullscreen
  style it skips `Pulse` and sends each due interval directly to the takeover.
- `daemon` runs policy in goroutines and calls `hud.Run` **last, on the main
  goroutine** (main.go already locks it to the OS thread).
- go.mod is pinned (cobra, yaml.v3, fatih/color); no new deps.
- Non-darwin / cgo-disabled builds get headless no-op `hud` stubs that log
  `[hud stub] ...` lines to stderr — daemon E2E asserts against stub logs
  without putting windows on a shared screen.

## Verification

```
go build ./... && go vet ./... && go test ./...
```

- Headless (CGO_ENABLED=0 stub build, short `$HOME` under /tmp): short
  `interval` test config → `focus daemon` in foreground → `set_focus` stub →
  `checkin` events tick (one per interval, absorbed while a screen is up) →
  drifted ack is the only thing that moves distractions → done ack logs done +
  set → `focus stats` renders.
- Visual: `go run ./hud/demo -pill -checkin -auto "f,type:next thing,enter"`
  (or `-pill -pulse 2 -takeover` for pulse style) with `-snap` self-snapshots —
  `screencapture` from agent shells silently omits app windows.
