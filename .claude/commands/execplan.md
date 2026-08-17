---
description: Write an ExecPlan for a complex change, following .agent/PLANS.md
argument-hint: <what to plan>
---

Write an ExecPlan for: $ARGUMENTS

Do this before writing any implementation code.

1. Read `.agent/PLANS.md` in full and treat it as binding: the five
   non-negotiable requirements, every required section, and the writing
   guidelines.
2. Investigate the repository until you can write the plan without guessing.
   Read the packages the change touches, the tests that cover them, and
   `README.md`'s safety policy. Run read-only commands (`go doc`, `ffprobe`,
   `rg`) as needed. Do not modify source files in this step.
3. Copy `.agent/plans/TEMPLATE.md` to
   `.agent/plans/<today's date as YYYY-MM-DD>-<short-slug>.md` and fill in every
   section. Use `date -u +%F` for the date rather than assuming it.
4. Make it genuinely self-contained: exact repo-relative paths, exact
   signatures and flags, every term defined, every required fact restated inside
   the plan instead of linked. Prose over bullet lists, except in Progress.
5. Break Progress into steps each finishable in one sitting, and make
   Validation and acceptance concrete — the real commands, with the numbers you
   expect them to print.

When the file is written, report its path and summarise the approach, the main
risks, and any open questions you need answered before implementation. Do not
start implementing until the plan is reviewed.
