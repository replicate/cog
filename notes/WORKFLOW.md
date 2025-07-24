# Task Workflow Guide

A lightweight system for managing Cog development tasks with Claude Code.

## Quick Start

```bash
/task my-feature-name          # Create new task
# Edit the generated file with your thinking
/work my-feature-name          # Execute the task  
/done my-feature-name          # Archive when complete
```

## The Philosophy

**Your job**: Think about the problem, define the goal, set constraints  
**LLM's job**: Plan the implementation, execute the work, track progress

This separates strategic thinking (you) from tactical execution (LLM), letting you spend more time on the interesting problems.

## Step-by-Step Workflow

### 1. Start a New Task
```bash
/task uv-block-fix
```
Creates `notes/tasks/uv-block-fix.md` with this template:

```markdown
# Task: uv-block-fix

## What's Up
[Brain dump the problem/goal here - keep it casual]

## Constraints & Context  
- **Type**: Bug | Feature | Refactor | Investigation
- **Backwards compat**: Must maintain | Can break | Don't care
- **Affected area**: [which components/stacks]

## Success Looks Like
- [ ] [concrete outcome 1]
- [ ] [concrete outcome 2]

## OK to Ignore
- [stuff that's out of scope or can be deferred]

## Open Questions
- [things you're unsure about]

---
## Session Progress
[LLM appends discoveries, decisions, next steps here]
```

### 2. Fill Out Your Task
Edit the file with your thinking. Be casual but specific:

```markdown
## What's Up
UV block is doing too much - complex conditional logic, shell commands 
that break in minimal containers, and not leveraging UV's native capabilities.
Want to simplify to a clean UV-native approach.

## Constraints & Context
- **Type**: Refactor
- **Backwards compat**: Can break (internal API)  
- **Affected area**: pkg/cogpack/stacks/python/uv.go

## Success Looks Like
- [ ] Simplified UV block logic (fewer conditionals)
- [ ] Uses UV native commands instead of shell workarounds
- [ ] All existing Python tests still pass
- [ ] UV lockfile generation works reliably

## OK to Ignore  
- Performance optimization
- Support for legacy pip-only projects

## Open Questions
- Should we convert legacy projects to UV format or handle separately?
- What's the minimal UV command set we actually need?
```

### 3. Execute the Task
```bash
/work uv-block-fix
```

The LLM will:
- Read your task document
- Read relevant context (CLAUDE.md, project docs, code)
- Create an execution plan
- Ask for your approval
- Execute the work systematically
- Update the task document with progress

### 4. Complete the Task
```bash
/done uv-block-fix "simplified UV block, fixed env var bug"
```

This will:
- Archive the task to `notes/tasks/completed/uv-block-fix.md`
- Add any follow-up items to `notes/follow-ups.md`
- Give you a completion summary

## File Organization

```
.claude/
└── commands/                 # Slash commands
    ├── task.md              # /task command
    ├── work.md              # /work command  
    └── done.md              # /done command
notes/
├── WORKFLOW.md              # This guide
├── _TASK_TEMPLATE.md        # Template for new tasks
├── follow-ups.md            # Future work items
├── tasks/                   # Active tasks
│   ├── my-feature.md        # Work in progress
│   └── completed/           # Archived tasks
│       └── old-feature.md   # Completed work
└── [other context docs...]
```

## Tips & Best Practices

### Writing Good Task Descriptions
- **Start with symptoms**: "Build fails with X error" vs "Fix the build"
- **Be concrete about success**: Specific outcomes, not vague goals
- **List constraints upfront**: Backwards compatibility, affected areas
- **Capture your uncertainty**: Open questions help the LLM ask the right things

### Scope Management
- Scope **will** evolve - that's expected and good
- LLM appends discoveries to "Session Progress" section
- Use "OK to Ignore" to park scope creep
- `/done` captures follow-ups for future tasks

### Multiple Tasks
- Each task gets its own file: `/task feature-a`, `/task bug-fix-b`
- Work on one at a time with `/work task-name`
- Archive completed work to keep `notes/tasks/` clean

### Integration with Existing Docs
- The `/work` command automatically reads CLAUDE.md and project-specific docs
- For cogpack work, it also reads pkg/cogpack/CLAUDE.md
- Your task document provides the specific context for this work
- Context docs provide the broader architectural understanding

## Command Reference

| Command | Purpose | Example |
|---------|---------|---------|
| `/task <name>` | Create new task file | `/task gpu-support` |
| `/work [name]` | Execute task (uses most recent if no name) | `/work gpu-support` |
| `/done [name] ["summary"]` | Archive completed task | `/done gpu-support "added CUDA detection"` |

## Troubleshooting

**"Task file not found"**: Make sure you created it with `/task` first  
**"Multiple tasks found"**: Specify the task name: `/work specific-task-name`  
**"No context found"**: Ensure CLAUDE.md exists in your project root

---

*This workflow works across all Cog development - cogpack, CLI, Python SDK, etc.*