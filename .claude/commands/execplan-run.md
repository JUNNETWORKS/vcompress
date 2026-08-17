---
description: Execute (or resume) an ExecPlan, keeping the plan updated as you go
argument-hint: [path to plan, or blank for the newest unfinished one]
---

Execute the ExecPlan: $ARGUMENTS

If no path was given, list `.agent/plans/` and pick the newest plan with
unchecked Progress items; state which one you chose before starting.

1. Read `.agent/PLANS.md` and then the plan itself, top to bottom. If the plan
   is not self-contained enough to implement — a missing signature, an undefined
   term, an unverifiable acceptance criterion — fix the plan first, then
   continue.
2. Work the Progress list in order. After each completed step: check the box
   with a UTC timestamp (`date -u +'%Y-%m-%d %H:%MZ'`), and write into the plan
   anything that surprised you, quoting real output, plus a Decision log entry
   for any choice you made that a reviewer might question.
3. Validate as you go using the plan's Validation and acceptance section, and at
   minimum `mise run check` before declaring a step done. Run
   `mise run build-windows` when anything under `internal/fsutil` changed, and
   `mise run test-integration` when encoding, validation or CRF selection
   changed.
4. If the design turns out to be wrong, stop and revise the plan — all affected
   sections, not just the tail — record why in the Decision log, then resume.
   Do not silently drift from the plan.
5. When every Progress item is checked, fill in Outcomes and retrospective:
   what shipped, what is still open, what you would do differently, and where
   the plan diverged from its original design. Paste the real command output
   into Artifacts and notes.

Report honestly: if a validation step failed or was skipped, say so with the
output.
