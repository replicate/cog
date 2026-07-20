# Playground Parity Checklist

This checklist is the behavioral contract for replacing the React playground.
Core behavior should be covered by Node tests; browser-only behavior is verified
with the cmux browser API before the replacement is considered complete.
The boxes are reusable verification prompts, not a release-status ledger.

## Connection

- [ ] Load `/config`, use its target and version values, and retain the fallback target.
- [ ] Never overwrite a manually edited target with a late config response.
- [ ] Connect by button and Enter; ignore blank targets.
- [ ] Retry failed schema requests and poll model health with the existing deadlines.
- [ ] Abort stale schema and health requests when the target changes.
- [ ] Display setup status/logs and Cog, coglet, SDK, and Python versions.
- [ ] Open the loaded schema as formatted JSON and revoke stale object URLs.
- [ ] Keep targets isolated between browser tabs.

## Inputs

- [ ] Select prediction or training input schemas and capabilities correctly.
- [ ] Order fields by `x-order` and preserve declaration order for ties.
- [ ] Preserve explicit defaults including `false` and `null`.
- [ ] Default required enum and Boolean inputs exactly as before.
- [ ] Render enums, URI/file, password, string, integer, number, Boolean, and structured controls.
- [ ] Preserve typed enum values such as `1` versus `"1"`.
- [ ] Include and omit optional fields without retaining stale busy/invalid state.
- [ ] Display descriptions, deprecation, required state, defaults, and constraints.
- [ ] Read files as data URIs with the 16 MiB limit and stale-read protection.
- [ ] Display image, audio, and video previews for matching data URIs.
- [ ] Preserve malformed structured JSON locally until corrected.
- [ ] Keep Form and JSON representations synchronized through the last valid object.
- [ ] Restore canonical JSON when leaving JSON mode with malformed input.
- [ ] Format JSON, reset defaults, and handle models with no inputs.

## Validation

- [ ] Validate only when Run is requested and retain existing validation messages.
- [ ] Reject non-object and recursively invalid JSON input.
- [ ] Preserve draft-04 refs, compositions, formats, nullable schemas, and nested paths.
- [ ] Normalize supported Python regular-expression syntax.
- [ ] Drop browser-incompatible patterns and leave the server authoritative.
- [ ] Validate ordinary and data URIs with current behavior.
- [ ] Treat required blank strings and empty arrays as missing.
- [ ] Suppress noisy union branch issues and deduplicate path/message pairs.
- [ ] Compile once per connected schema in an isolated worker.
- [ ] Ignore stale validation responses after edits, reset, or reconnection.
- [ ] Report worker creation, post, timeout, and schema failures without crashing.

## Predictions

- [ ] Default to Stream when supported and Sync otherwise.
- [ ] Support POST and custom-ID PUT requests with encoded IDs.
- [ ] Support Sync, Stream, and Async/webhook modes.
- [ ] Select webhook events and always include `completed` in the request filter.
- [ ] Open the webhook event stream before submitting an async prediction.
- [ ] Preserve webhook-before-acknowledgement ordering.
- [ ] Handle start, output, metric, log, error, completed, and unknown stream events.
- [ ] Preserve progressive output instead of replacing it with terminal output.
- [ ] Fail a stream that ends without a terminal event.
- [ ] Stop locally immediately and issue best-effort API cancellation.
- [ ] Ignore every late response or event from canceled, reset, or superseded runs.
- [ ] Reset input, prediction ID, output, errors, trace, and transport buffers.
- [ ] Preserve all response/error/SSE/file/output size and retention limits.

## Output And Inspector

- [ ] Render waiting, no-output, text, structured JSON, and string-array output correctly.
- [ ] Concatenate plain string chunks and render media-like arrays item by item.
- [ ] Render data images/audio/video, downloadable data files, and safe HTTP(S) links.
- [ ] Preserve output whitespace, running cursor, keyboard scrolling, and tail following.
- [ ] Display metrics in an accessible table.
- [ ] Preserve Output, Raw, optional Logs, Timeline, and Request views.
- [ ] Keep visited panels mounted and selected view stable across runs.
- [ ] Display exact raw SSE frames and normalized terminal logs.
- [ ] Preserve request method, endpoint, headers, body, start time, status, and timing.
- [ ] Decode only server-supplied model response-header metadata.
- [ ] Preserve bounded trace payloads, compaction, omission markers, and event ordering.

## JSON Editor

- [ ] Preserve line numbers, fold gutter/keymap, history, bracket matching, and active-line styling.
- [ ] Preserve JSON syntax highlighting and line wrapping.
- [ ] Preserve accessible editable, read-only, disabled, invalid, and described states.
- [ ] Keep Tab available for browser focus navigation.
- [ ] Apply controlled updates without adding undo history or recursively firing changes.
- [ ] Preserve selection during controlled updates.
- [ ] Copy the full document and retain select-all fallback behavior.
- [ ] Auto-size between 80 and 320 px.
- [ ] Follow live output until the user scrolls upward or interacts with the scrollbar.

## Appearance And Accessibility

- [ ] Use the vendored Kumo standalone stylesheet and semantic color tokens.
- [ ] Preserve light/dark initialization without a flash and persist manual selection.
- [ ] Preserve header, sticky toolbar, two-panel layout, cards, badges, tabs, and editors.
- [ ] Fit all primary controls at a 320 px viewport without document overflow.
- [ ] Preserve visible focus rings and reduced-motion behavior.
- [ ] Preserve labels, fieldsets, required/invalid/disabled/deprecated semantics.
- [ ] Preserve tablist/tab/tabpanel relationships and keyboard activation.
- [ ] Preserve live status, alert, busy-region, table, and details semantics.
- [ ] Never insert model-controlled content through `innerHTML`.

## Go Boundary

- [ ] `go run`, `go test`, `go build`, and release builds include the real playground assets.
- [ ] No Go task or release workflow installs Node or builds frontend assets.
- [ ] Existing loopback, origin, proxy-header, redirect, webhook, SSE, and CSP tests pass.
- [ ] The fixed validation-worker path receives the isolated `unsafe-eval` CSP only.
- [ ] Static modules, styles, worker, vendor files, and licenses have correct MIME types.
