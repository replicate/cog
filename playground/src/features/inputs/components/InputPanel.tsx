import { Button } from "@cloudflare/kumo/components/button";

import { SegmentedTabs, tabId, tabPanelId } from "@/components/SegmentedTabs";
import { LazyJsonEditor } from "@/components/editor/LazyJsonEditor";
import { InputForm } from "@/features/inputs/components/InputForm";
import type { PlaygroundInputState } from "@/features/inputs/hooks/usePlaygroundInput";
import type { OpenAPIDocument, OpenAPISchema } from "@/types/openapi";

type Props = {
  document?: OpenAPIDocument;
  schema?: OpenAPISchema;
  schemaError: string;
  state: PlaygroundInputState;
};

export function InputPanel({ document, schema, schemaError, state }: Props) {
  return (
    <section id="input-panel">
      <div className="panel-head">
        <h2>Input</h2>
        <SegmentedTabs
          id="input-mode"
          label="Input editor mode"
          items={[
            { value: "form", label: "Form" },
            { value: "json", label: "JSON" },
          ]}
          value={state.inputMode}
          onChange={state.changeInputMode}
        />
      </div>
      {schemaError && <output className="notice visible">{schemaError}</output>}
      <div
        id={tabPanelId("input-mode", state.inputMode)}
        role="tabpanel"
        aria-labelledby={tabId("input-mode", state.inputMode)}
        tabIndex={0}
      >
        {document && schema && state.inputMode === "form" && (
          <InputForm
            key={state.formRevision}
            document={document}
            schema={schema}
            value={state.input}
            errors={state.validationIssues}
            onChange={state.changeFormInput}
            onBusyChange={state.setFormBusy}
            onValidityChange={state.setFormValid}
          />
        )}
        {state.inputMode === "json" && (
          <div id="json-container">
            <LazyJsonEditor
              value={state.jsonInput}
              label="Prediction input JSON"
              className="json-input"
              describedBy={
                [
                  state.jsonError ? "json-input-error" : undefined,
                  state.validationIssues.length > 0 ? "input-validation-errors" : undefined,
                ]
                  .filter(Boolean)
                  .join(" ") || undefined
              }
              invalid={Boolean(state.jsonError) || state.validationIssues.length > 0}
              onChange={state.changeJsonInput}
            />
            {state.jsonError && (
              <small id="json-input-error" className="field-error" role="alert">
                {state.jsonError}
              </small>
            )}
            <Button size="sm" variant="ghost" onClick={state.formatJsonInput}>
              Format
            </Button>
          </div>
        )}
        <div id="input-validation-errors" aria-live="polite" aria-atomic="true">
          {state.validationIssues.length > 0 && (
            <div className="error-container">
              <strong>Input does not match the OpenAPI schema.</strong>
              <ul className="validation-summary">
                {state.validationIssues.map((error) => (
                  <li key={`${error.path}:${error.keyword}:${error.message}`}>
                    <code>{error.path}</code>: {error.message}
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
      </div>
    </section>
  );
}
