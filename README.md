<div align="center">

# 🎯 focus

**A focus overlay with full-screen check-ins for macOS.**

*Keep the main thing visible — and check in on it, full-screen, on your cadence.*

</div>

`focus` keeps your highest-priority task visible in a quiet ambient pill. On a cadence you pick (15m, 30m…), a full-screen check-in dissolves your work behind a blur until you answer. Every check-in and response is logged locally so distraction trends come from what actually happened.

- **Always-visible priority** — a subtle, directly draggable HUD follows you across Spaces
- **Full-screen check-ins** — the reminder you can't not notice, on your `interval`
- **One-keystroke answers** — `⏎` still on it, `D` drifted, `N` change focus, `F` done (and type what's next right there)
- **Idle-aware** — an empty desk never gets a check-in; returning does
- **Honest local stats** — day-over-day and week-over-week distraction charts from append-only JSONL; routine check-ins never count as distractions
- **Pulse style (optional)** — replace every-interval check-ins with the v1 glow-pulse escalation ladder
- **No Dock icon** — the daemon runs as an `LSUIElement` app managed by launchd

---

## Install

Requires macOS and Go 1.25+.

```sh
git clone https://github.com/shadowfax92/focus
cd focus
make install
```

This builds `~/Applications/Focus.app`, symlinks the CLI into `$GOPATH/bin`, and starts a LaunchAgent. The app is deliberately separate from `mac-notify`: focus owns the persistent priority HUD; mac-notify remains a notification queue.

## Quick start

```sh
focus set "ship the onboarding PR"
focus status
focus pause 45m              # meeting mode
focus resume
focus done
focus stats
```

## Commands

| Command | Description |
|---------|-------------|
| `focus set "…"` | Set or replace the current priority |
| `focus done` / `focus clear` | Complete and hide the current priority |
| `focus status` | Show focus text, elapsed time, escalation rung, and pause state |
| `focus pause <duration>` | Pause reminders, using Go durations such as `45m` or `2h` |
| `focus resume` | Resume the HUD and reminder timer |
| `focus ack [--drifted]` | Acknowledge the current reminder from the CLI |
| `focus stats` | Compare today with yesterday and rank distractions by focus |
| `focus stats --detailed` / `focus stats -d` | Add today's chronological event timeline in local time |
| `focus stats --days N` | Render a daily distraction bar chart |
| `focus stats weeks` | Show week-over-week trends and sparklines |
| `focus quotes add\|list\|rm` | Manage takeover quotes |
| `focus config` | Print the resolved configuration |
| `focus install` / `uninstall` | Manage the app bundle and LaunchAgent |
| `focus daemon` | Run the daemon in the foreground for development |

The default stats view keeps the headline metrics compact, then lists each focus used today with its distraction count, highest first. Repeated exact focus text is combined; activity from legacy history before its first `set` appears as `(unattributed)` rather than being guessed.

`--detailed` / `-d` adds today's full event timeline with local, human-readable timestamps and the focus active for each event. It is intentionally limited to today's view and cannot be combined with `--days` or `weeks`. All stats views accept `--json`; detailed JSON preserves the normal summary fields and adds a structured `timeline` array.

## The check-in screen

Every `interval` the screen blurs over and shows your focus, a quote, and four keys:

| Key | Meaning |
|-----|---------|
| `⏎` | Still on it |
| `D` | Drifted — this is what counts as a distraction |
| `N` | Change the focus (edit it inline) |
| `F` | Done — logs completion, then type the next focus right there (`⏎` starts it; `⏎` on an empty field means nothing next and the screen stays quiet until the next `focus set`; `⎋` backs out) |

The ambient pill remains visible between check-ins. While a check-in is up, further intervals are absorbed — there is never a second screen stacked on the first. Routine check-ins are *not* distractions; only `D` moves the metric. Fewer is better, so negative changes render green.

### Pulse style

`reminder_style: pulse` keeps the ambient pill but replaces direct check-ins with the v1 ladder: brightening pulses each unacknowledged reminder, and the full-screen takeover only after `escalate_after` ignored pulses (a shown takeover then also counts as a distraction). Click the pill to acknowledge — left-click on task, ⌥-click drifted.

## Config

`~/.config/focus/config.yaml`:

```yaml
reminder_style: fullscreen   # fullscreen (default) | pulse
interval: 15m                # reminder cadence (15m, 30m, …)
idle_pause_minutes: 5
idle_opacity: 0.3             # ambient pill opacity in either style
position:
  preset: top-center
  x: 0
  y: 0
quotes:
  - The main thing is to keep the main thing the main thing.

# pulse reminder style only:
pulse_seconds: 8
escalate_after: 2
breathing_gate_seconds: 3    # escalation takeovers; check-ins arm when faded in
```

`interval` uses Go-style durations. It drives direct check-ins in fullscreen style and pulse-ladder ticks in pulse style. Position presets are `top-center`, `top-right`, `top-left`, and `custom`; the visible pill is directly draggable with no modifier key, and dragging it persists a custom position. A click without movement between reminders does nothing.

## Local data

| Path | Purpose |
|------|---------|
| `~/.config/focus/config.yaml` | User configuration and quotes |
| `~/.local/state/focus/current.json` | Current focus, pause, rung, and custom position |
| `~/.local/share/focus/events.jsonl` | Append-only event history used for every stats view |
| `~/.focus.sock` | Per-user daemon socket |

No aggregate stats database exists. Delete or edit the JSONL only if you intentionally want to change history.

## Development

```sh
go build ./...
go vet ./...
go test ./...
```

To run without launchd:

```sh
focus daemon
```

Then use the CLI from another terminal. `focus uninstall` removes the app, CLI symlink, plist, and socket while keeping configuration and event history.

---

<div align="center">

> Personal tool built for my own workflow. Feel free to fork and adapt.

</div>
