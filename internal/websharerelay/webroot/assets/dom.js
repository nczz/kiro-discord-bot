export function el(tag, options = {}) {
    const node = document.createElement(tag);
    if (options.className)
        node.className = options.className;
    if (options.text !== undefined)
        node.textContent = options.text;
    for (const [name, value] of Object.entries(options.attrs ?? {}))
        node.setAttribute(name, value);
    for (const child of options.children ?? [])
        node.append(child);
    return node;
}
export function clear(node) {
    while (node.firstChild)
        node.firstChild.remove();
}
export function formatBytes(bytes) {
    if (!Number.isFinite(bytes) || bytes < 0)
        return "-";
    const units = ["B", "KiB", "MiB", "GiB"];
    let value = bytes;
    let unit = 0;
    while (value >= 1024 && unit < units.length - 1) {
        value /= 1024;
        unit += 1;
    }
    return `${value.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
}
