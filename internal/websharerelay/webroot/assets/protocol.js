export const PROTOCOL_VERSION = 1;
export function hasCapability(capabilities, capability) {
    return Boolean(capabilities?.[capability]);
}
