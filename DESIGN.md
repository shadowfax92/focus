# focus — design

One ambient focus pill keeps the main thing visible, gently glowing between
check-ins; every `interval` a full-screen check-in asks whether you're still
on it — with honest day-over-day distraction stats. The v1 pulse escalation
ladder survives as an opt-in cadence.

Deliberately **separate from mac-notify** (that stays a pure notification queue).
This tool is a persistent, stateful HUD with its own daemon, socket, and app bundle.

## Reminder styles — `reminder_style`

- **`fullscreen` (default).** The ambient pill stays visible at
  `idle_opacity`. Every `pulse_interval` (default 5m), it plays the existing
  rung-0 glow for `pulse_seconds` as a passive nudge: no rung growth,
  escalation, or required ack. Clicking a glowing pill can still ack it
  (left-click on_task, ⌥-click drifted), without moving either reminder timer.
  Every `interval` (15m/30m — user's call) the full-screen
  check-in appears directly: no pulse rungs, no `escalate_after` gating. While
  one is up, further ticks are absorbed (never a second screen, never rung
  growth — an idle stretch spent staring at one doesn't re-fire it either).
  Routine check-ins skip the breathing circle
  (`Gate: 0`): a gate 4×/hour would be pure friction — `breathing_gate_seconds`
  applies to pulse-mode escalations only. The keys still arm only once the 2s
  fade-in completes, so in-flight typing can never ack a screen that isn't
  visible yet. Setting a focus does **not** fire an instant screen; the first
  glow comes a full `pulse_interval` later, and the first check-in comes a
  full `interval` later. A due check-in always wins over a due glow; no glows
  fire while a check-in is up. Any lingering glow ends when the screen opens.
  Set, resume, and check-in acknowledgements restart both schedules; optional
  glow acknowledgements leave them alone. The idle guard and pause/no-focus
  guards suppress both. `pulse_interval: 0` disables these nudges, as does a
  `pulse_interval` greater than or equal to `interval`.
- **`pulse`.** The v1 cadence, unchanged: the same ambient pill shows glow
  pulses climbing rungs each unacked tick, with a takeover after
  `escalate_after` ignored pulses.

Both styles share the ambient pill, idle guard, pause/resume, ack vocabulary,
and takeover screen itself.

## UX spec

### Pill (both styles)

- Liquid Glass capsule, its own look rather than mac-notify's (the
  owner-approved Paper design "02 · Liquid"): a macOS 26
  `NSGlassEffectView` (Regular style) ~460pt wide and 46pt tall on one line.
  Longer focus text wraps and grows the pill downward as a rounded rect.
  The glass adapts to whatever is behind it, with white ink over dark content
  and near-black ink over light pages. Before macOS 26 it falls back to an
  always-dark `NSVisualEffectView` (`.hudWindow`).
- Always on top (`NSStatusWindowLevel + 1`), joins all Spaces, full-screen
  auxiliary, stationary.
- Content: 18pt heavy focus text on the left, and on the right a nested time
  chip with the bright elapsed time `47m` (ticks once a minute). An optional
  focus budget adds a dim `/ 45m` to the chip (`9m / 45m`). Once the budget
  is exceeded, a red `+12m` sub-chip with a white ring is appended
  (`57m / 45m` then `+12m`). Only the overage is tinted.
  Elapsed remains wall-clock time since set, including paused/idle time.
  Budgeted focuses show `0m / 45m` immediately. Unbudgeted focuses show no
  chip during their first minute. Budgets add no alerts.
- Glow (pulse ladder and passive glows alike): an amber halo outside the
  glass plus an amber wash and rim inside it, breathing on the pulse cadence
  below. Colour roles never overlap: amber only ever means "look at me", and
  over-budget red is pushed red enough that it can't read as the glow.
  Rung 0, the frequent passive nudge, stays gentle. Rung 2+'s glow is the hard
  ceiling, and no rung may glow brighter or wider than it.
- Idle: opacity = `idle_opacity` (default 0.30). Whenever the pill is visible,
  it accepts mouse input and can be dragged directly with no modifier key.
- On drag end the new origin is reported to the daemon and persisted. Config
  also supports presets: `top-center` (default), `top-right`, `top-left`.
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
  on_task**, **⌥-click = drifted**. Between pulses, the ambient pill remains
  directly draggable; a click without a drag is ignored.
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
  avg ack latency, DoD %, followed by focus text ranked by its distraction
  count. Exact repeated focus text combines; legacy events without focus
  context remain explicitly unattributed. **Down is good**: fewer
  distractions renders green.
- `focus stats --detailed` / `-d` — append today's complete event timeline in
  stable chronological order, with local 12-hour timestamps, readable labels,
  active focus context, and rung/latency metadata. Detailed mode is today-only.
- `focus stats --days N` — vertical bar chart of distractions/day, exactly the
  gh-stats look (`/Users/shadowfax/code/clis/gh-stats/render/render.go` —
  `VerticalBars`, `Sparkline`, `FormatPctInt`, fatih/color).
- `focus stats weeks` — WoW comparison rows + per-week sparklines + streak line
  ("3 days improving").
- `--json` on all stats views. Day objects add structured focus summaries;
  detailed today JSON keeps the regular top-level fields and adds `timeline`.

## CLI

```
focus set "ship the onboarding PR"   # set/replace focus; pill appears
focus set "Fix setup" 45m            # optional positive Go-style budget
focus set "Write doc" 1h30m
focus done                           # logs done, clears focus, hides pill
focus clear                          # alias of done
focus status                         # focus + elapsed + budget/overage + rung + pause
focus pause 45m                      # meeting mode
focus resume
focus ack [--drifted]                # ack from the CLI (default on_task)
focus stats [--detailed | -d] [--json]
focus stats --days N [--json]
focus stats weeks [--json]
focus quotes add "..." | list | rm <n>
focus config                         # print active config (disk when offline)
focus restart                        # restart launchd; state survives
focus install | uninstall            # app bundle + launchd agent
focus daemon                         # run daemon in foreground (dev)
```

## Config — `~/.config/focus/config.yaml` (defaults shown)

```yaml
reminder_style: fullscreen   # fullscreen | pulse
interval: 15m
pulse_interval: 5m           # fullscreen nudges; 0 or >= interval disables
pulse_seconds: 8             # rung-0 glow duration in both styles
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

`interval` and `pulse_interval` use Go-style durations. `pulse_interval` must
be nonnegative and is ignored by `reminder_style: pulse`. Existing configs
that omit it inherit the 5m default; `focus config` prints the resolved value.

### Automatic reload and restart

Config edits apply automatically within ~2s. A dedicated daemon goroutine
polls `os.Stat` every **2s**, comparing mtime, size, permissions, and file identity
(inode on macOS), so rename-style editor saves are detected without a filesystem
watch dependency. Startup and reload use the same `config.LoadFrom` validation.
The watcher stats again after reading; a file replaced/modified during the
load is retried on the next poll, rather than publishing a stale config or
transient parse error. Missing files resolve to defaults, matching startup.

File I/O runs outside the daemon mutex; publishing a validated config shares
the scheduler/IPC/HUD-input lock. Invalid YAML, invalid values, or read/stat
errors keep the last good config and are logged. `focus status` and
`focus config` report `config error: ... (running with previous config)`
until a valid load clears it, even without an active focus. The config IPC
verb returns the daemon's active snapshot; offline/older-daemon CLI use falls
back to resolving the file. Failed loads stay eligible for retry even when
the file is unchanged, so transient I/O failures recover automatically;
duplicate error messages are not logged on every poll.

The scheduler retains separate window origins for the interval and passive
glow timers. Reload changes a deadline only when its cadence changes:

- The new deadline is `window origin + new duration`, preserving elapsed
  time. If already elapsed, it is deferred to `reload time + 1s`; only a
  subsequent scheduler beat presents the reminder. Reload itself never
  shows a check-in or starts a glow.
- Unchanged cadence keys leave both deadlines untouched, including a
  quote-only edit. Changing `interval` also re-evaluates whether passive
  glows are enabled, without restarting an otherwise unchanged glow timer.
- Setting `pulse_interval: 0` or `>= interval` cancels passive cadence and
  immediately ends its current glow/optional ack window. Enabling nudges
  starts a full new glow window without moving the primary deadline.
- A style change uses the new style for the next reminder without moving
  the primary deadline. An active ladder pulse is retired so its mandatory
  ack/rung state cannot leak into fullscreen nudges. An open takeover stays
  until answered. Quote, gate, escalation, idle guard, and glow-duration
  values govern subsequent behavior; a visible glow keeps its original
  duration and acknowledgement expiry.
- Changing an armed idle guard's threshold clears its previous latch and
  re-evaluates the idle stretch under the new policy. That edit cannot count
  as user activity or fabricate an `idle_return`; any newly allowed overdue
  reminder waits one scheduler beat without changing cadence deadlines.

`hud.ApplyConfig` copies its inputs across the cgo/async handoff and applies
opacity and future glow duration on Cocoa's main queue. The next takeover
carries its configured gate explicitly, without restarting an existing gate.
Preset position changes also run there; a saved dragged `custom` position
wins, both under the daemon lock and on the UI thread if the drag callback is
still in flight. `hud.StopPulse` cancels glow animation generations without
dismissing an unanswered takeover.

`focus restart` checks for the installed/loaded LaunchAgent and runs
`launchctl kickstart -k gui/<uid>/com.focus.daemon`, leaving persisted state
and history in place. `focus install` (and foreground startup) exclusively
creates a commented default config when missing. The header explains reload
within ~2s and `focus restart`; existing configs and their comments are kept.

Runtime state (survives daemon restart): `~/.local/state/focus/current.json` —
current focus text, set-at timestamp, optional budget (`budget_ns`, exact
nanoseconds), paused-until, saved custom position. Missing/zero budget means
unbudgeted. A new focus (CLI set or inline refocus/next task) replaces any old
budget; completion clears it. Pause, resume, and daemon restart preserve it.

## IPC

Unix socket `~/.focus.sock`, JSON request/response, one connection per command
(same pattern as `/Users/shadowfax/code/clis/mac-notify/ipc/`). Verbs:
`set, done, status, config, pause, resume, ack, ping`. The CLI prints a helpful error
(`focus install` / `focus daemon`) when the daemon is down.
`set` accepts an optional `budget` Go-style duration string; `status` returns
the exact commitment as `budget_ns` when present.
It also returns positive overage as `overage_ns`, computed before truncating
elapsed to the legacy whole-second `elapsed_seconds` field.

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
  style it owns separate glow and check-in deadlines, checking the latter
  first. Passive glows reuse the existing rung-0 rendering without entering
  the escalation state machine. Each carries a process-local reminder ID
  through Cocoa and back with an optional click, so a delayed glow click can
  never acknowledge a newer glow or check-in. Their optional ack window is
  ephemeral and ends with the glow, pause, focus change, or check-in; it is never restored
  as a pending reminder after daemon restart. Check-ins keep their existing
  interval and acknowledgement behavior.
- `daemon` runs scheduling, config polling, and IPC in goroutines and calls `hud.Run` **last, on the main
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
  `screencapture` from agent shells silently omits app windows. The pill
  snapshot is the window server's own composite (real glass, glow and window
  alpha). Add `-backdrop <image>` to put known dark or light content right
  behind the pill, and `-idle-opacity 1` to see a resting pill at full strength.
