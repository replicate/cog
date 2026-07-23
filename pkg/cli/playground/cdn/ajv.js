// @ts-check
// Ajv is loaded from cdnjs instead of being vendored. cdnjs ships a
// self-contained UMD bundle (all of Ajv's internals in one file), which is
// imported here for its global side effect and re-exported as an ES module.
// Source: https://github.com/ajv-validator/ajv (MIT)
//
// Notes:
//   - This is Ajv's default JSON Schema draft-07 build. The draft-04 build
//     (ajv-draft-04) can't be loaded from a CDN as an ES module: Ajv compiles
//     schemas by generating code at runtime, and CDNs that split it across
//     modules end up with two copies of its internal codegen, breaking the
//     `instanceof` checks it relies on. The self-contained bundle avoids that.
//   - ajv-formats is not bundled, so non-custom formats (date-time, email, ...)
//     are treated as annotations rather than validated. Structural validation
//     (types, required, enum, pattern, min/max, oneOf, ...) is unaffected.

import "https://cdnjs.cloudflare.com/ajax/libs/ajv/8.17.1/ajv7.bundle.min.js";

const ajvModule = /** @type {{ default?: unknown, Ajv?: unknown }} */ (
  /** @type {Record<string, unknown>} */ (globalThis).ajv7
);

export const Ajv = /** @type {import("./ajv").Ajv } */ (
  ajvModule.default ?? ajvModule.Ajv ?? ajvModule
);
