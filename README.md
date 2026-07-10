<div align="center">

# 🎯 focus

**A floating focus HUD for macOS.**

*One glowing pill with your current priority — escalating reminders when you drift, stats to prove you're drifting less.*

</div>

`focus` keeps your highest-priority task visible without turning it into another notification. A low-opacity pill floats above every Space, wakes up on a configurable cadence, and escalates when reminders go unacknowledged. Every pulse and response is logged locally so distraction trends come from what actually happened.

- **Always-visible priority** — a subtle, click-through HUD follows you across Spaces
- **Escalation ladder** — longer glow pulses lead to a full-screen reset when ignored
- **Fast acknowledgments** — click the pill, use the takeover keyboard controls, or run `focus ack`
- **Idle-aware reminders** — an empty desk never grows the escalation rung
- **Honest local stats** — day-over-day and week-over-week distraction charts from append-only JSONL
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
| `focus ack [--drifted]` | Acknowledge the current reminder |
| `focus stats` | Compare today with yesterday |
| `focus stats --days N` | Render a daily distraction bar chart |
| `focus stats weeks` | Show week-over-week trends and sparklines |
| `focus quotes add\|list\|rm` | Manage takeover quotes |
| `focus config` | Print the resolved configuration |
| `focus install` / `uninstall` | Manage the app bundle and LaunchAgent |
| `focus daemon` | Run the daemon in the foreground for development |

All stats views accept `--json`.

## Escalation

With the defaults, a new focus gets a rung 0 pulse. The next unacknowledged reminder uses rung 1; after two ignored pulses, the following reminder becomes the full-screen takeover. Any acknowledgment resets the next reminder to rung 0.

The distraction metric is intentionally narrow: a drifted acknowledgment or a takeover shown. Fewer is better, so negative changes render green.

## Config

`~/.config/focus/config.yaml`:

```yaml
interval: 15m
pulse_seconds: 8
escalate_after: 2
breathing_gate_seconds: 3
idle_opacity: 0.3
idle_pause_minutes: 5
position:
  preset: top-center
  x: 0
  y: 0
quotes:
  - The main thing is to keep the main thing the main thing.
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
