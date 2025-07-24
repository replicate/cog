# Task: testing

## What's Up
I'm just testing out the task commands. Let's do something simple. Run the full test suite with INTEGRATION=1 and tell me what tests are failing.

## Constraints & Context
- **Type**: Bug | Feature | Refactor | Investigation  
- **Backwards compat**: Must maintain | Can break | Don't care
- **Affected area**: [which components/stacks]

## Success Looks Like
- the task workflow is working
- you correctly identified the failing test

## OK to Ignore
- don't fix the tests

## Open Questions  
- I don't know if the Constraints & Context section is working. I don't really care about Type or Backwards compat. Can we make that more useful? Or drop it?

---
## Session Progress

**Initial Plan**: Run integration tests to identify failing tests and test workflow  

**Test Results**: Found 1 failing integration test:
- **TestPythonStack_SourceCodeIsCopied** in `pkg/cogpack/integration_test.go`
- **Error**: `r8.im/cog-base:python3.13.4-ubuntu22.04-dev: not found`
- **Root cause**: Missing base image - the test is trying to pull a Cog base image that doesn't exist in the registry

**Workflow Status**: ✅ Task workflow is working correctly - LLM successfully read task, executed plan, and is updating progress

**Template Update**: ✅ Simplified "Constraints & Context" → "Context" with more casual prompt

**Task Complete**: 
- Identified failing test: `TestPythonStack_SourceCodeIsCopied` (missing base image)
- Confirmed workflow works end-to-end
- Updated template based on feedback

---
**Completed**: 2025-07-23
**Summary**: Successfully tested task workflow system and identified failing integration test due to missing base images
