import { PROTOCOL_VERSION } from "./protocol.js";

const encoder = new TextEncoder();
const decoder = new TextDecoder();
const SECRET_VIEW_BYTES = 1 + 32;
const SECRET_WRITE_BYTES = 1 + 32 + 32;
const KEY_INFO = "kdb-webshare-v1";

export type FrameType = 1 | 2 | 3 | 4 | 5;
export type Direction = "guest_to_host" | "host_to_guest";

export interface ParsedJoinLink {
  roomID: string;
  roomKey: Uint8Array<ArrayBuffer>;
  writeToken?: Uint8Array<ArrayBuffer>;
  canWrite: boolean;
}

export interface CryptoEnvelope {
  v: typeof PROTOCOL_VERSION;
  t: FrameType;
  seq: number;
  peer: number;
  p: string;
}

export interface SessionKeys {
  hostToGuest: CryptoKey;
  guestToHost: CryptoKey;
}

export function parseJoinFragment(hash = window.location.hash): ParsedJoinLink {
  const fragment = hash.startsWith("#") ? hash.slice(1) : hash;
  const prefix = "/join/";
  if (!fragment.startsWith(prefix)) {
    throw new Error("invalid_join_fragment");
  }
  const body = fragment.slice(prefix.length);
  const separator = body.indexOf(".");
  if (separator <= 0 || separator === body.length - 1) {
    throw new Error("invalid_join_fragment");
  }
  const roomID = decodeURIComponent(body.slice(0, separator));
  if (!/^[A-Za-z0-9_-]{8,128}$/.test(roomID)) {
    throw new Error("invalid_room_id");
  }
  const secret = base64urlToBytes(body.slice(separator + 1));
  if (secret.length !== SECRET_VIEW_BYTES && secret.length !== SECRET_WRITE_BYTES) {
    throw new Error("invalid_secret_length");
  }
  if (secret[0] !== PROTOCOL_VERSION) {
    throw new Error("unsupported_secret_version");
  }
  const roomKey = secret.slice(1, 33);
  if (secret.length === SECRET_WRITE_BYTES) {
    return { roomID, roomKey, writeToken: secret.slice(33), canWrite: true };
  }
  return { roomID, roomKey, canWrite: false };
}

export async function deriveSessionKeys(roomID: string, roomKey: Uint8Array<ArrayBuffer>): Promise<SessionKeys> {
  const material = await crypto.subtle.importKey("raw", roomKey, "HKDF", false, ["deriveBits"]);
  const bits = await crypto.subtle.deriveBits(
    {
      name: "HKDF",
      hash: "SHA-256",
      salt: encoder.encode(roomID),
      info: encoder.encode(KEY_INFO),
    },
    material,
    512,
  );
  const bytes = new Uint8Array(bits);
  const hostToGuest = await crypto.subtle.importKey("raw", bytes.slice(0, 32), { name: "AES-GCM" }, false, ["encrypt", "decrypt"]);
  const guestToHost = await crypto.subtle.importKey("raw", bytes.slice(32, 64), { name: "AES-GCM" }, false, ["encrypt", "decrypt"]);
  return { hostToGuest, guestToHost };
}

export async function sealJSON(value: unknown, key: CryptoKey, roomID: string, peerID: number, seq: number, frameType: FrameType, direction: Direction): Promise<Uint8Array> {
  const plaintext = encoder.encode(JSON.stringify(value));
  const nonce = nonceFor(peerID, seq);
  const additionalData = associatedData(roomID, direction, peerID, seq, frameType);
  const ciphertext = await crypto.subtle.encrypt({ name: "AES-GCM", iv: nonce, additionalData }, key, plaintext);
  const envelope: CryptoEnvelope = {
    v: PROTOCOL_VERSION,
    t: frameType,
    seq,
    peer: peerID,
    p: bytesToBase64url(new Uint8Array(ciphertext)),
  };
  return encoder.encode(JSON.stringify(envelope));
}

export async function openJSON<T>(payload: Uint8Array, key: CryptoKey, roomID: string, expectedPeerID: number, frameType: FrameType, direction: Direction): Promise<T> {
  const envelope = JSON.parse(decoder.decode(payload)) as CryptoEnvelope;
  if (envelope.v !== PROTOCOL_VERSION || envelope.t !== frameType) {
    throw new Error("invalid_crypto_envelope");
  }
  if (!Number.isSafeInteger(envelope.seq) || envelope.seq < 0 || !Number.isSafeInteger(envelope.peer) || envelope.peer < 0) {
    throw new Error("invalid_crypto_envelope_counter");
  }
  if (envelope.peer !== expectedPeerID) {
    throw new Error("crypto_peer_mismatch");
  }
  const nonce = nonceFor(envelope.peer, envelope.seq);
  const additionalData = associatedData(roomID, direction, envelope.peer, envelope.seq, envelope.t);
  const plaintext = await crypto.subtle.decrypt({ name: "AES-GCM", iv: nonce, additionalData }, key, base64urlToBytes(envelope.p));
  return JSON.parse(decoder.decode(plaintext)) as T;
}

export function writeTokenProof(writeToken: Uint8Array<ArrayBuffer> | undefined): string | undefined {
  return writeToken ? bytesToBase64url(writeToken) : undefined;
}

export function bytesToBase64url(bytes: Uint8Array): string {
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary).replaceAll("+", "-").replaceAll("/", "_").replace(/=+$/u, "");
}

export function base64urlToBytes(value: string): Uint8Array<ArrayBuffer> {
  if (!/^[A-Za-z0-9_-]*$/.test(value)) {
    throw new Error("invalid_base64url");
  }
  const padded = value.replaceAll("-", "+").replaceAll("_", "/") + "===".slice((value.length + 3) % 4);
  const binary = atob(padded);
  const out = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) out[i] = binary.charCodeAt(i);
  return out;
}

export async function sha256Base64url(bytes: Uint8Array): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", copyBytes(bytes));
  return bytesToBase64url(new Uint8Array(digest));
}

function nonceFor(peerID: number, seq: number): Uint8Array<ArrayBuffer> {
  const nonce = new Uint8Array(12);
  const view = new DataView(nonce.buffer);
  view.setUint32(0, peerID, false);
  setUint64(view, 4, seq);
  return nonce;
}

function associatedData(roomID: string, direction: Direction, peerID: number, seq: number, frameType: FrameType): Uint8Array<ArrayBuffer> {
  const peer = new Uint8Array(4);
  new DataView(peer.buffer).setUint32(0, peerID, false);
  const sequence = new Uint8Array(8);
  setUint64(new DataView(sequence.buffer), 0, seq);
  return concatBytes(
    encoder.encode(KEY_INFO),
    new Uint8Array([0]),
    encoder.encode(roomID),
    new Uint8Array([0]),
    encoder.encode(direction),
    new Uint8Array([0]),
    peer,
    new Uint8Array([0]),
    sequence,
    new Uint8Array([0]),
    new Uint8Array([frameTypeID(frameType)]),
  );
}

function frameTypeID(frameType: FrameType): number {
  return frameType;
}

function setUint64(view: DataView, offset: number, value: number): void {
  const bigint = BigInt(value);
  view.setUint32(offset, Number((bigint >> 32n) & 0xffffffffn), false);
  view.setUint32(offset + 4, Number(bigint & 0xffffffffn), false);
}

function concatBytes(...chunks: Uint8Array<ArrayBufferLike>[]): Uint8Array<ArrayBuffer> {
  const total = chunks.reduce((sum, chunk) => sum + chunk.length, 0);
  const out = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    out.set(chunk, offset);
    offset += chunk.length;
  }
  return out;
}

function copyBytes(bytes: Uint8Array<ArrayBufferLike>): Uint8Array<ArrayBuffer> {
  return new Uint8Array(bytes);
}
