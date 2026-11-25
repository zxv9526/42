import { resolveApiUrl } from './networkConfig';

export interface UploadedVoiceResult {
  audioUrl?: string;
  audio?: string;
}

export function blobToDataUrl(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onloadend = () => resolve(String(reader.result || ''));
    reader.onerror = () => reject(reader.error || new Error('FileReader failed'));
    reader.readAsDataURL(blob);
  });
}

/**
 * Upload recorded voice audio over HTTP so WebSocket control frames (moves, ping/pong)
 * never suffer head-of-line blocking behind multi-hundred-KB Base64 audio payloads.
 * Falls back to inline data URL only if the HTTP upload fails and payload is within limits.
 */
export async function uploadVoiceBlob(blob: Blob): Promise<UploadedVoiceResult> {
  const dataUrl = await blobToDataUrl(blob);
  if (!dataUrl) return {};

  try {
    const controller = typeof AbortController !== 'undefined' ? new AbortController() : null;
    const timer = controller ? setTimeout(() => controller.abort(), 6000) : null;
    try {
      const res = await fetch(resolveApiUrl('/api/voice/upload'), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ audio: dataUrl }),
        signal: controller?.signal,
      });
      if (res.ok) {
        const data = await res.json().catch(() => null);
        if (data && data.success && typeof data.voiceUrl === 'string' && data.voiceUrl.startsWith('/api/voice/')) {
          return { audioUrl: data.voiceUrl };
        }
      }
    } finally {
      if (timer) clearTimeout(timer);
    }
  } catch {
    // Fallback below
  }

  if (dataUrl.length <= 450_000) {
    return { audio: dataUrl };
  }
  return {};
}

export function resolveVoicePlayUrl(rawUrl?: string | null): string {
  if (!rawUrl || typeof rawUrl !== 'string') return '';
  if (rawUrl.startsWith('data:') || rawUrl.startsWith('blob:') || rawUrl.startsWith('http://') || rawUrl.startsWith('https://')) {
    return rawUrl;
  }
  if (rawUrl.startsWith('/')) {
    return resolveApiUrl(rawUrl);
  }
  return rawUrl;
}
