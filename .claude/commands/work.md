# Start Working on Task

You are beginning work on a Cog development task that has been defined in a task document.

## Instructions

1. **Identify the task** - Look for task name in command arguments, or if none provided, look for files in `notes/tasks/` directory
2. **Read the task document** at `notes/tasks/{task-name}.md`
3. **Read relevant context** - Check CLAUDE.md and any project-specific docs (like CLAUDE.project.md, CLAUDE.cogpack.md)
4. **Create an execution plan** based on the task requirements
5. **Get user approval** before proceeding with implementation
6. **Execute the work** systematically
7. **Update the task document** - Append progress, discoveries, and decisions to the "Session Progress" section

## Context Reading Priority
1. The specific task document
2. CLAUDE.md (main project context)
3. Any project-specific context docs (CLAUDE.project.md, CLAUDE.cogpack.md, etc.)
4. Relevant code files mentioned in the task

## Planning Approach
- Break down the task into concrete steps
- Identify potential risks or unknowns
- Propose testing strategy
- Ask clarifying questions if the task is ambiguous

## Progress Updates
Append to the task document's "Session Progress" section with:
- **Plan**: Initial approach
- **Discovery X**: Key findings during work
- **Decision X**: Important choices made
- **Next**: Follow-up items identified

## Response Format
Start with:
```
Working on task: {task-name}

[Summary of what you understand from the task]

## Execution Plan
1. [step 1]
2. [step 2]
...

Ready to proceed? Or should I clarify anything first?
```