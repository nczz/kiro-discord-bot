import { bytesToBase64url, sha256Base64url } from "./crypto.js";
export function selectedAttachmentRefs(state) {
    return state.pending.map((attachment) => ({ ...attachment, ref: attachment.ref ?? attachment.attachmentRef }));
}
export async function continueAcceptedUpload(state, uploadID, dispatch) {
    const index = state.queued.findIndex((upload) => upload.uploadID === uploadID);
    if (index < 0)
        return;
    const upload = state.queued[index];
    if (!upload)
        return;
    state.queued.splice(index, 1);
    state.inProgress.set(uploadID, upload);
    const chunkSize = 48 * 1024;
    for (let offset = 0, seq = 0; offset < upload.bytes.length; offset += chunkSize, seq += 1) {
        await dispatch({ type: "upload_chunk", uploadID, seq, bytes: bytesToBase64url(upload.bytes.slice(offset, offset + chunkSize)) });
    }
    await dispatch({ type: "upload_finish", uploadID });
}
export async function queueUploads(files, state, dispatch) {
    if (!files)
        return;
    for (const file of files) {
        const bytes = new Uint8Array(await file.arrayBuffer());
        const sha256 = await sha256Base64url(bytes);
        const mime = file.type || "application/octet-stream";
        const uploadID = crypto.randomUUID();
        state.queued.push({ uploadID, filename: file.name, mime, size: file.size, bytes, sha256 });
        await dispatch({ type: "upload_init", uploadID, name: file.name, mime, size: file.size, sha256 });
    }
}
export function downloadURL(chunks, mime) {
    const bytes = [];
    for (const chunk of chunks) {
        const binary = atob(chunk.replaceAll("-", "+").replaceAll("_", "/") + "===".slice((chunk.length + 3) % 4));
        for (let i = 0; i < binary.length; i += 1)
            bytes.push(binary.charCodeAt(i));
    }
    return URL.createObjectURL(new Blob([new Uint8Array(bytes)], { type: mime || "application/octet-stream" }));
}
