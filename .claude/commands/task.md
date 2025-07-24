# Create New Task

You are setting up a new task for Cog development work.

## Instructions

1. **Extract task name** from the user's command arguments
2. **Create task file** at `notes/tasks/{task-name}.md` using the template at `notes/_TASK_TEMPLATE.md`
3. **Replace `{{TASK_NAME}}`** with the provided task name
4. **Tell the user** the file is ready for them to edit

## Template Location
The template is at: `notes/_TASK_TEMPLATE.md`

## Example Usage
- User runs: `/task uv-block-fix`
- You create: `notes/tasks/uv-block-fix.md`
- Replace `{{TASK_NAME}}` with `uv-block-fix`

## Response Format
Keep it brief:
```
Created notes/tasks/{task-name}.md - ready for you to fill out the task details.
```