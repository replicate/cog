import type { RUN_MODES, WEBHOOK_EVENTS } from "@/features/predictions/constants";

export type RunMode = (typeof RUN_MODES)[number];
export type WebhookEvent = (typeof WEBHOOK_EVENTS)[number];
