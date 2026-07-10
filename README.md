<div align="center">

# 🎯 focus

**A full-screen focus check-in for macOS.**

*Nothing on screen until it's time — then the whole screen asks whether you're still on the main thing.*

</div>

`focus` keeps your highest-priority task honest without another notification fighting for a corner of your eye. Nothing is visible between reminders; on a cadence you pick (15m, 30m…), a full-screen check-in dissolves your work behind a blur until you answer. Every check-in and response is logged locally so distraction trends come from what actually happened.

- **Full-screen check-ins** — the reminder you can't not notice, on your `interval`
- **One-keystroke answers** — `⏎` still on it, `D` drifted, `N` change focus, `F` done (and type what's next right there)
- **Idle-aware** — an empty desk never gets a check-in; returning does
- **Honest local stats** — day-over-day and week-over-week distraction charts from append-only JSONL; routine check-ins never count as distractions
- **Pulse style (optional)** — the v1 ambient glow pill + escalation ladder, one config key away
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
focus ack                    # still on task
focus ack --drifted          # count a distraction
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
| `focus stats` | Compare today with yesterday |
| `focus stats --days N` | Render a daily distraction bar chart |
| `focus stats weeks` | Show week-over-week trends and sparklines |
| `focus quotes add\|list\|rm` | Manage takeover quotes |
| `focus config` | Print the resolved configuration |
| `focus install` / `uninstall` | Manage the app bundle and LaunchAgent |
| `focus daemon` | Run the daemon in the foreground for development |

All stats views accept `--json`.

## The check-in screen

Every `interval` the screen blurs over and shows your focus, a quote, and four keys:

| Key | Meaning |
|-----|---------|
| `⏎` | Still on it |
| `D` | Drifted — this is what counts as a distraction |
| `N` | Change the focus (edit it inline) |
| `F` | Done — logs completion, then type the next focus right there (`⏎` starts it, `⎋` means nothing next: the screen closes and stays quiet until the next `focus set`) |

While a check-in is up, further intervals are absorbed — there is never a second screen stacked on the first. Routine check-ins are *not* distractions; only `D` moves the metric. Fewer is better, so negative changes render green.

### Pulse style

`reminder_style: pulse` restores the v1 model: an ambient glow pill at `idle_opacity`, brightening pulses each unacknowledged reminder, and the full-screen takeover only after `escalate_after` ignored pulses (a shown takeover then also counts as a distraction). Click the pill to acknowledge — left-click on task, ⌥-click drifted.

## Config

`~/.config/focus/config.yaml`:

```yaml
reminder_style: fullscreen   # fullscreen (default) | pulse
interval: 15m                # how often the check-in appears (15m, 30m, …)
idle_pause_minutes: 5
quotes:
  - The main thing is to keep the main thing the main thing.

# pulse style only:
pulse_seconds: 8
escalate_after: 2
breathing_gate_seconds: 3    # escalation takeovers; routine check-ins arm instantly
idle_opacity: 0
position:
  preset: top-center
  x: 0
  y: 0
```

`interval` uses Go-style durations. Position presets are `top-center`, `top-right`, `top-left`, and `custom`; dragging the interactive pill persists a custom position.

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
