package tools

const todoDescription = "Track multi-step progress within this conversation. Use for planning and checklists — NOT for background execution."

// TodoPrompt is the behavioral guidance for the todo tool, injected into the system prompt.
const TodoPrompt = `## Todo Tool Guide

Use the todo tool to track multi-step progress within this conversation. This is a session-level checklist — NOT for background or async execution (use the task tool for that).

## When to Use

- Multi-step work requiring 3+ distinct steps
- User explicitly asks for a todo list or checklist
- User provides multiple items to be done
- After receiving new instructions — capture requirements as todos immediately
- Mark a todo as in_progress BEFORE starting it; mark completed AFTER finishing

## When NOT to Use

- Single, straightforward operation
- Trivial work completable in < 3 steps
- Purely conversational or informational queries

## States

- **pending**: Not yet started
- **in_progress**: Currently working on (ONE at a time)
- **completed**: Finished successfully

## Rules

- Update status in real-time as you work
- Mark complete IMMEDIATELY after finishing — don't batch
- Only ONE todo in_progress at a time
- Remove irrelevant todos by omitting them
- ONLY mark completed when FULLY done — keep as in_progress if blocked or partial
- Break complex work into specific, actionable items

## Capabilities

- Create, update, or remove todos in a single call
- Pass an empty array to clear all todos`
