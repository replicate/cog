export type HealthResponse = {
  status?: string;
  user_healthcheck_error?: string;
  setup?: { status?: string; logs?: string };
  version?: { coglet?: string; python_sdk?: string; python?: string };
};
