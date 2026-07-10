# focus

Read DESIGN.md first — it is the spec. (The v1 two-lane parallel build and its
frozen `hud/hud.go` contract are over; change the hud API together with its
callers when the work needs it.)

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
