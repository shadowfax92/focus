# focus — design

One floating glow pill that shows your current priority, an escalation ladder of
reminders that survives habituation, and honest day-over-day distraction stats.

Deliberately **separate from mac-notify** (that stays a pure notification queue).
This tool is a persistent, stateful HUD with its own daemon, socket, and app bundle.

## UX spec

### Pill (ambient state)

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

### Pulse ladder

- Every `interval` (default 15m) the daemon fires a pulse at the current rung:
  - rung 0: breathe to full opacity + glow for `pulse_seconds` (default 8s)
  - rung 1: ~20s, brighter glow
  - rung 2+: constant glow until acked or the next tick
- The rung increments each tick that passes with no ack since the previous
  pulse; any ack resets rung to 0.
- After `escalate_after` (default 2) consecutive unacked pulses, the next
  reminder is the **takeover** instead of a pulse.
- Acking on the pill while it is interactive: **left-click = on_task**,
  **⌥-click = drifted**.
- Idle guard: if the user has been idle longer than `idle_pause_minutes`
  (default 5), ticks are skipped entirely — no rung growth, no takeover at an
  empty desk. On return from an idle stretch ≥ `idle_pause_minutes`, fire an
  immediate welcome-back pulse (rung 0) and log `idle_return`.
- `pause` hides the pill and stops ticks until `resume` (or the duration lapses).

### Takeover (top of the ladder)

- Full-screen panel on the main screen. `NSVisualEffectView` blur — the work
  visibly dissolves behind it — fading in over ~2s.
- Centered column: glowing focus text (large), a random quote from config
  below it, and a dim mirror footer, e.g.
  `2nd escalation today · yesterday: 5 · 43m on task`
  (both strings are computed by the daemon and passed in).
- Breathing gate: a breathing-circle animation for `breathing_gate_seconds`
  (default 3, 0 = off) before the ack keys arm. Key hints appear after the gate.
- Keyboard-first; the panel becomes key and **swallows all keystrokes**:
  - `Enter` → on_task
  - `D` → drifted
  - `N` → inline text field to retype/replace the focus → refocus ack (the new
    text rides along; the daemon sets it as the current focus)
- No Esc, no mouse required. On ack: fade out, restore whatever was key before.
- Never shown while the idle guard is active.

## Acks and stats

Event log: append-only JSONL at `~/.local/share/focus/events.jsonl`:

```json
{"ts":"2026-07-09T14:05:00Z","type":"pulse","rung":1}
{"ts":"...","type":"ack","kind":"drifted","rung":1,"latency_s":4.2}
{"ts":"...","type":"set","text":"ship onboarding PR"}
```

`type`: `set | pulse | ack | escalation | done | pause | resume | idle_return`
`kind` (acks): `on_task | drifted | refocus`

**Distraction (the metric) = a `drifted` ack OR an `escalation` shown.**
Everything is derived at read time from the JSONL; no aggregate state.

- `focus stats` — today vs yesterday: distractions, pulses, acks, avg ack
  latency, DoD %. **Down is good**: fewer distractions renders green.
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
interval: 15m
pulse_seconds: 8
escalate_after: 2
breathing_gate_seconds: 3
idle_opacity: 0.30
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

## Architecture and lane ownership

**Policy in Go, pixels in Objective-C.** The daemon decides *when* anything
happens; `hud` only draws and reports input.

```
main.go            tiny; init() locks the main OS thread (Cocoa needs it)
cmd/               cobra CLI                              — LANE A
ipc/               socket client/server/protocol          — LANE A
config/            yaml load/save, defaults               — LANE A
store/             events.jsonl append + stats derivation — LANE A
render/            terminal charts (gh-stats style)       — LANE A
daemon/            tick scheduler, escalation state machine,
                   idle detection (one tiny cgo file for
                   CGEventSourceSecondsSinceLastEventType) — LANE A
hud/               frozen Go API + objc implementation
                   + hud/demo visual harness              — LANE B
Makefile, plist    build, Focus.app (LSUIElement), launchd — LANE A
```

- **LANE A never edits `hud/**`. LANE B never edits anything outside `hud/**`.**
- `hud/hud.go` signatures are **FROZEN**. A change requires orchestrator
  sign-off first (ping the orchestrator pane), then both lanes move together.
- `daemon` runs policy in goroutines and calls `hud.Run` **last, on the main
  goroutine** (main.go already locks it to the OS thread).
- go.mod is pre-pinned (cobra, yaml.v3, fatih/color). Lane B adds no deps.
- The scaffold ships `hud` as headless no-op stubs that log
  `[hud stub] ...` lines to stderr — Lane A verifies the daemon end-to-end
  against stub logs before Lane B lands.

## Verification

```
go build ./... && go vet ./... && go test ./...
```

- Lane A headless: `interval: 10s` test config → `focus daemon` in foreground →
  `set` → watch stub log lines climb rungs → `escalation` after 2 unacked →
  `focus ack` resets → events.jsonl rows correct → `focus stats` renders.
- Lane B visual: `go run ./hud/demo -pill -pulse 2 -takeover`, then
  `screencapture -x /tmp/shot.png` and inspect the image.
