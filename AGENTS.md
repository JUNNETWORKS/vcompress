# AGENTS.md

Guidance for coding agents working in this repository. `README.md` describes
what vcompress does and the safety policy it must uphold.

## ExecPlans

When writing complex features or significant refactors, use an **ExecPlan** (as
described in `.agent/PLANS.md`) from design to implementation. Write the plan
first, to `.agent/plans/YYYY-MM-DD-short-slug.md`, starting from
`.agent/plans/TEMPLATE.md`; then implement it, keeping the plan's Progress,
Surprises and discoveries, and Decision log sections current as you go.

Small, self-contained changes do not need a plan — just make them.

## Commands

This project uses `mise` (see `mise.toml`); the tasks mirror CI.

```bash
mise run check            # go test ./... plus go vet ./...  (what CI gates on)
mise run build            # host binary
mise run build-windows    # GOOS=windows GOARCH=amd64 compile check
mise run test-integration # real FFmpeg/libx265 round trip; needs ffmpeg+ffprobe on PATH
```

## Ground rules

- Never weaken a validation gate, a codec skip rule or the file replacement
  path in `internal/processor` and `internal/fsutil` without an ExecPlan that
  states the risk and how the change was proven safe. The point of this tool is
  to not destroy people's source video.
- Keep the Windows and Unix implementations in `internal/fsutil` in sync, and
  confirm both still compile.
- Documentation and code comments in this repository are written in English.
