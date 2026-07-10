# focus

Read DESIGN.md first — it is the spec, including the two-lane ownership map
for the parallel build (LANE A: everything except hud/; LANE B: hud/ only).

- `hud/hud.go` exported signatures are FROZEN — do not change them
  unilaterally; ping the orchestrator pane instead.
- Policy lives in Go (`daemon/`), pixels in Objective-C (`hud/`).
- Verify with: `go build ./... && go vet ./... && go test ./...`
- UI verification gotcha: `screencapture` from agent shells lacks Screen
  Recording permission — app windows are silently omitted (wallpaper + menu
  bar only). Verify on-screen presence via `CGWindowListCopyWindowInfo`
  (owner/layer/alpha/bounds need no permission) or `hud/demo -snap`
  self-snapshots; do not trust screenshots to prove a window is (in)visible.
- Unix socket paths cap at ~104 bytes — keep test `$HOME` short (mktemp under
  /tmp), or the daemon fails to bind `~/.focus.sock`.
- No git remote yet: commit locally on the current branch, skip pushing.
- Reference implementations on this machine:
  - ../mac-notify — ipc/config/launchd/app-bundle patterns, overlay visuals
  - ../gh-stats — terminal stats/chart rendering style
