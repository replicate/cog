# Local Agent Instructions

## Commit Per Bead Rule

**CRITICAL**: When closing a bead, you MUST commit the associated code changes BEFORE closing the bead.

Workflow:
1. Complete the work for a bead/task
2. `git add` the relevant files
3. `git commit -m "descriptive message referencing bead-id"`
4. THEN `bd close <bead-id>`

Do NOT batch multiple beads into a single commit. Each bead should have its own commit so the git history reflects the logical units of work tracked in beads.

Exception: If multiple beads are truly atomic and inseparable (rare), commit them together and close them together with a note explaining why.

## Write Tests Concurrently

**CRITICAL**: When implementing new functionality, write tests AT THE SAME TIME, not as a separate phase.

- Unit tests for pure logic (Rust native `#[test]`)
- Use `insta` for snapshot testing where appropriate
- Two distinct snapshot trees planned:
  - Unit/component snapshots in crate test directories
  - Integration snapshots (separate tree, for end-to-end tests later)

Phase 4-pre will backfill tests for existing code, but going forward: no feature is complete without tests.

## Never Commit AGENTS.local.md

**NEVER** commit this file (AGENTS.local.md). It is for local agent instructions only and should remain untracked.
