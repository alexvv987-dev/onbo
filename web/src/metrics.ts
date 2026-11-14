type MetricMetadata = Record<string, string>;

export type MetricName =
  | "plan_opened"
  | "task_completed"
  | "question_asked"
  | "source_shown"
  | "draft_created"
  | "request_confirmed"
  | "request_result"
  | "request_refreshed";

export function trackMetric(name: MetricName, metadata: MetricMetadata = {}) {
  void fetch("/api/events", {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name, metadata }),
    keepalive: true,
  }).catch(() => undefined);
}
