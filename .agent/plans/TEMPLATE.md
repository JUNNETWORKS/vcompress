# <Title of the change>

<!--
Copy this file to .agent/plans/YYYY-MM-DD-short-slug.md and fill it in.
The requirements, format and guidelines are defined in .agent/PLANS.md.
Delete these comments and any section guidance as you write.
Every section below is required.
-->

## Purpose and big picture

What changes for someone running `vcompress`, in their terms. Why this is worth
doing. What is explicitly out of scope.

## Context and orientation

Which packages are involved and what each is responsible for. Full
repo-relative paths of every file that matters. The behaviour as it exists
today. Definitions of every term a newcomer would not know.

## Interfaces and dependencies

Exact signatures, struct fields, flags with defaults and validation, FFmpeg /
ffprobe arguments and their meaning, external version requirements.

## Plan of work

Prose walking through the change in the order it should be made, and why that
order is correct.

## Concrete steps

Exact commands, the working directory for each, and a short expected transcript
where the output matters.

```bash
mise run check
```

## Validation and acceptance

How anyone proves this works, and what "done" means observably. Include the
expected numbers.

- [ ] `mise run check`
- [ ] `mise run build-windows`
- [ ] `mise run test-integration`
- [ ] `--dry-run` pass over real media, with log evidence (if discovery, policy
      or CRF selection changed)
- [ ] The safety policy in `README.md` still holds

## Idempotence and recovery

What re-running does, what is not safely repeatable, and how to return to a
known-good state — including cleanup of `.<name>.ffmpeg-compressing-*`
temporary outputs.

## Progress

- [ ] First small step.

## Surprises and discoveries

(Fill in as they happen. Quote the actual output.)

## Decision log

`YYYY-MM-DD — <author>: <decision>. <rationale, including rejected alternatives>.`

## Artifacts and notes

Transcripts, `ffmpeg-compress.log` excerpts, `ffprobe` before/after, diffs,
sizes, SSIM values.

## Outcomes and retrospective

(Written at the end.) What shipped, what is still open, what you would do
differently, and where the plan diverged from its original design.
