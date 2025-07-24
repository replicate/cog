# Complete Task

You are wrapping up a completed Cog development task.

## Instructions

1. **Identify the task** - Look for task name in command arguments, or if none provided, look for the most recent task file
2. **Read the current task document** at `notes/tasks/{task-name}.md`
3. **Archive the completed work**:
   - Move the task file to `notes/tasks/completed/{task-name}.md`
   - Add completion date and summary to the file
4. **Update follow-ups** - Add any identified next steps to `notes/follow-ups.md`
5. **Clean summary** - Provide brief completion summary to user

## Completion Process

### 1. Archive Task File
Move `notes/tasks/{task-name}.md` to `notes/tasks/completed/{task-name}.md` and add:
```markdown
---
**Completed**: [DATE]
**Summary**: [brief summary from user input or session progress]
```

### 2. Update Follow-ups
If the task identified follow-up work, add to `notes/follow-ups.md`:
```markdown
## [Task Name] Follow-ups (completed [DATE])
- [ ] [follow-up item 1]
- [ ] [follow-up item 2]
```

### 3. Create Completed Directory
Create `notes/tasks/completed/` if it doesn't exist.

## Command Usage
- `/done` - Complete the most recent task
- `/done task-name` - Complete specific named task  
- `/done task-name "summary text"` - Complete with custom summary

## Response Format
```
✅ Completed task: {task-name}
📁 Archived to: notes/tasks/completed/{task-name}.md
📝 Added [N] follow-ups to notes/follow-ups.md

[Brief summary of what was accomplished]
```