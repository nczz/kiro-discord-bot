export type AgentLifecycleStatus = "queued" | "running" | "complete" | "interrupted" | "error" | "unknown";
export type ActiveAgentLifecycleStatus = "queued" | "running";

export function classifyAgentLifecycle(status: string): AgentLifecycleStatus {
  const normalized = status.trim().toLowerCase();
  switch (normalized) {
    case "queued":
    case "running":
    case "complete":
    case "interrupted":
    case "error":
      return normalized;
    default:
      return "unknown";
  }
}

export function activeAgentLifecycle(status: AgentLifecycleStatus): ActiveAgentLifecycleStatus | undefined {
  return status === "queued" || status === "running" ? status : undefined;
}
