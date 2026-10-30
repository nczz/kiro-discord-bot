export function classifyAgentLifecycle(status) {
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
export function activeAgentLifecycle(status) {
    return status === "queued" || status === "running" ? status : undefined;
}
