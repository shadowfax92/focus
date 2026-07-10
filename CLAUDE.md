# focus

Read DESIGN.md first — it is the spec, including the two-lane ownership map
for the parallel build (LANE A: everything except hud/; LANE B: hud/ only).

- `hud/hud.go` exported signatures are FROZEN — do not change them
  unilaterally; ping the orchestrator pane instead.
- Policy lives in Go (`daemon/`), pixels in Objective-C (`hud/`).
- Verify with: `go build ./... && go vet ./... && go test ./...`
- No git remote yet: commit locally on the current branch, skip pushing.
- Reference implementations on this machine:
  - ../mac-notify — ipc/config/launchd/app-bundle patterns, overlay visuals
  - ../gh-stats — terminal stats/chart rendering style
