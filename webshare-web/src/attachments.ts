import { bytesToBase64url, sha256Base64url } from "./crypto.js";
import type { AttachmentMetadata, AttachmentRef, ClientAction } from "./protocol.js";

interface QueuedUpload {
  uploadID: string;
  filename: string;
  mime: string;
  size: number;
  bytes: Uint8Array;
  sha256: string;
}

export interface UploadState {
  pending: AttachmentRef[];
  queued: QueuedUpload[];
  inProgress: Map<string, QueuedUpload>;
  downloads: Map<string, { metadata: AttachmentMetadata; chunks: string[]; done: boolean }>;
}


export function selectedAttachmentRefs(state: UploadState): AttachmentRef[] {
  return state.pending.map((attachment) => ({ ...attachment, ref: attachment.ref ?? attachment.attachmentRef }));
}


export async function continueAcceptedUpload(state: UploadState, uploadID: string, dispatch: (action: ClientAction) => void | Promise<void>): Promise<void> {
  const index = state.queued.findIndex((upload) => upload.uploadID === uploadID);
  if (index < 0) return;
  const upload = state.queued[index];
  if (!upload) return;
  state.queued.splice(index, 1);
  state.inProgress.set(uploadID, upload);
  const chunkSize = 48 * 1024;
  for (let offset = 0, seq = 0; offset < upload.bytes.length; offset += chunkSize, seq += 1) {
    await dispatch({ type: "upload_chunk", uploadID, seq, bytes: bytesToBase64url(upload.bytes.slice(offset, offset + chunkSize)) });
  }
  await dispatch({ type: "upload_finish", uploadID });
}

export async function queueUploads(files: FileList | null, state: UploadState, dispatch: (action: ClientAction) => void | Promise<void>): Promise<void> {
  if (!files) return;
  for (const file of files) {
    const bytes = new Uint8Array(await file.arrayBuffer());
    const sha256 = await sha256Base64url(bytes);
    const mime = file.type || "application/octet-stream";
    const uploadID = crypto.randomUUID();
    state.queued.push({ uploadID, filename: file.name, mime, size: file.size, bytes, sha256 });
    await dispatch({ type: "upload_init", uploadID, name: file.name, mime, size: file.size, sha256 });
  }
}

export function downloadURL(chunks: string[], mime?: string): string {
  const bytes: number[] = [];
  for (const chunk of chunks) {
    const binary = atob(chunk.replaceAll("-", "+").replaceAll("_", "/") + "===".slice((chunk.length + 3) % 4));
    for (let i = 0; i < binary.length; i += 1) bytes.push(binary.charCodeAt(i));
  }
  return URL.createObjectURL(new Blob([new Uint8Array(bytes)], { type: mime || "application/octet-stream" }));
}
