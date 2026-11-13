import type { AmbientPalette } from './types';

const DEFAULT_PALETTE: AmbientPalette = {
  primary: '42, 62, 118',
  secondary: '18, 86, 82',
  accent: '126, 214, 255',
};

const paletteCache = new Map<string, AmbientPalette>();

/**
 * Extracts two vibrant ambient colors + an accent tint from an album cover URL.
 */
export async function extractAmbientPalette(imageUrl: string): Promise<AmbientPalette> {
  if (!imageUrl) return DEFAULT_PALETTE;
  const cached = paletteCache.get(imageUrl);
  if (cached) return cached;

  try {
    const img = await loadImage(imageUrl);
    const canvas = document.createElement('canvas');
    const size = 32;
    canvas.width = size;
    canvas.height = size;
    const ctx = canvas.getContext('2d', { willReadFrequently: true });
    if (!ctx) return DEFAULT_PALETTE;

    ctx.drawImage(img, 0, 0, size, size);
    const { data } = ctx.getImageData(0, 0, size, size);

    interface Bucket {
      r: number;
      g: number;
      b: number;
      score: number;
      hue: number;
    }
    const candidates: Bucket[] = [];

    for (let i = 0; i < data.length; i += 4) {
      const r = data[i];
      const g = data[i + 1];
      const b = data[i + 2];
      const a = data[i + 3];
      if (a < 180) continue;

      const max = Math.max(r, g, b);
      const min = Math.min(r, g, b);
      const lum = (max + min) / 510;
      const sat = max === 0 ? 0 : (max - min) / max;

      // Skip near-black, near-white, or washed-out greys when possible.
      if (lum < 0.08 || lum > 0.92) continue;
      const score = sat * 2.2 + (1 - Math.abs(lum - 0.45)) * 0.9;
      const hue = computeHue(r, g, b, max, min);
      candidates.push({ r, g, b, score, hue });
    }

    if (candidates.length === 0) {
      return DEFAULT_PALETTE;
    }

    candidates.sort((a, b) => b.score - a.score);
    const first = candidates[0];
    let second = candidates.find((c) => hueDistance(c.hue, first.hue) > 42);
    if (!second) {
      second = candidates[Math.min(candidates.length - 1, Math.floor(candidates.length / 3))];
    }

    const primary = boostColor(first.r, first.g, first.b, 0.92);
    const secondary = boostColor(second.r, second.g, second.b, 0.85);
    const accent = brightenColor(first.r, first.g, first.b);

    const result: AmbientPalette = { primary, secondary, accent };
    paletteCache.set(imageUrl, result);
    return result;
  } catch {
    return DEFAULT_PALETTE;
  }
}

function loadImage(url: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const img = new Image();
    img.crossOrigin = 'anonymous';
    img.onload = () => resolve(img);
    img.onerror = () => reject(new Error('image load failed'));
    img.src = url;
  });
}

function computeHue(r: number, g: number, b: number, max: number, min: number): number {
  if (max === min) return 0;
  const d = max - min;
  let h = 0;
  if (max === r) h = (g - b) / d + (g < b ? 6 : 0);
  else if (max === g) h = (b - r) / d + 2;
  else h = (r - g) / d + 4;
  return h * 60;
}

function hueDistance(a: number, b: number): number {
  const diff = Math.abs(a - b);
  return Math.min(diff, 360 - diff);
}

function boostColor(r: number, g: number, b: number, scale: number): string {
  const clamp = (v: number) => Math.max(18, Math.min(220, Math.round(v * scale)));
  return `${clamp(r)}, ${clamp(g)}, ${clamp(b)}`;
}

function brightenColor(r: number, g: number, b: number): string {
  const mix = (v: number) => Math.min(255, Math.round(v + (255 - v) * 0.45));
  return `${mix(r)}, ${mix(g)}, ${mix(b)}`;
}

