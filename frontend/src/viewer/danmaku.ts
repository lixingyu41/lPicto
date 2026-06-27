export type DanmakuMode = 'scroll' | 'top' | 'bottom';

export interface DanmakuCue {
  id: string;
  start: number;
  end: number;
  text: string;
  mode: DanmakuMode;
  color: string;
  fontSize: number;
  displayDuration: number;
}

export interface PositionedDanmakuCue extends DanmakuCue {
  lane: number;
}

export function parseDanmakuText(text: string, format: string): DanmakuCue[] {
  const normalizedFormat = format.trim().toLowerCase();
  const trimmed = text.trim();
  if (normalizedFormat === 'bilibili' || looksLikeBilibiliXML(trimmed)) {
    return parseBilibiliXML(trimmed);
  }
  return parseWebVTT(trimmed);
}

export function layoutDanmaku(cues: DanmakuCue[], laneCount: number): PositionedDanmakuCue[] {
  const lanes = Array.from({ length: Math.max(1, laneCount) }, () => 0);
  const bottomLanes = Array.from({ length: Math.max(1, Math.floor(laneCount / 2)) }, () => 0);
  return [...cues]
    .sort((a, b) => a.start - b.start || a.id.localeCompare(b.id))
    .map((cue) => {
      const target = cue.mode === 'bottom' ? bottomLanes : lanes;
      const lane = claimLane(target, cue.start, cue.displayDuration);
      return { ...cue, lane };
    });
}

function claimLane(lanes: number[], start: number, duration: number) {
  let selected = 0;
  let earliest = Number.POSITIVE_INFINITY;
  for (let i = 0; i < lanes.length; i++) {
    if (lanes[i] <= start) {
      selected = i;
      break;
    }
    if (lanes[i] < earliest) {
      earliest = lanes[i];
      selected = i;
    }
  }
  lanes[selected] = start + Math.max(1.5, duration * 0.72);
  return selected;
}

function parseBilibiliXML(text: string): DanmakuCue[] {
  const parser = new DOMParser();
  const doc = parser.parseFromString(text, 'application/xml');
  if (doc.querySelector('parsererror')) return [];
  const nodes = Array.from(doc.querySelectorAll('d[p]'));
  return nodes
    .map((node, index): DanmakuCue | null => {
      const parts = (node.getAttribute('p') ?? '').split(',');
      const start = Number(parts[0]);
      if (!Number.isFinite(start) || start < 0) return null;
      const rawText = cleanDanmakuText(node.textContent ?? '');
      if (!rawText) return null;
      const mode = biliMode(Number(parts[1]));
      const fontSize = clampNumber(Number(parts[2]), 18, 34, 25);
      const color = decimalColor(parts[3]);
      const displayDuration = mode === 'scroll' ? scrollDuration(rawText) : 4.5;
      return {
        id: `bili-${index}-${start}`,
        start,
        end: start + displayDuration,
        text: rawText,
        mode,
        color,
        fontSize,
        displayDuration,
      };
    })
    .filter((cue): cue is DanmakuCue => cue !== null);
}

function parseWebVTT(text: string): DanmakuCue[] {
  const normalized = text.replace(/\r\n/g, '\n').replace(/\r/g, '\n');
  const blocks = normalized.split(/\n{2,}/);
  const cues: DanmakuCue[] = [];
  for (let index = 0; index < blocks.length; index++) {
    const lines = blocks[index]
      .split('\n')
      .map((line) => line.trim())
      .filter(Boolean);
    if (lines.length === 0 || lines[0].startsWith('WEBVTT') || lines[0].startsWith('NOTE')) continue;
    const timingIndex = lines.findIndex((line) => line.includes('-->'));
    if (timingIndex < 0) continue;
    const [startRaw, endRaw] = lines[timingIndex].split('-->').map((part) => part.trim());
    const start = parseCueTime(startRaw);
    const end = parseCueTime(endRaw.split(/\s+/)[0] ?? '');
    if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) continue;
    const rawText = cleanDanmakuText(lines.slice(timingIndex + 1).join(' '));
    if (!rawText) continue;
    const displayDuration = scrollDuration(rawText);
    cues.push({
      id: `vtt-${index}-${start}`,
      start,
      end,
      text: rawText,
      mode: 'scroll',
      color: '#ffffff',
      fontSize: 25,
      displayDuration,
    });
  }
  return cues;
}

function parseCueTime(value: string) {
  const parts = value.trim().split(':');
  if (parts.length < 2 || parts.length > 3) return Number.NaN;
  const secondsPart = parts.pop() ?? '';
  const seconds = Number(secondsPart.replace(',', '.'));
  const minutes = Number(parts.pop());
  const hours = parts.length > 0 ? Number(parts.pop()) : 0;
  if (![hours, minutes, seconds].every(Number.isFinite)) return Number.NaN;
  return hours * 3600 + minutes * 60 + seconds;
}

function looksLikeBilibiliXML(text: string) {
  return /<d\s+[^>]*p="/i.test(text);
}

function biliMode(value: number): DanmakuMode {
  if (value === 4) return 'bottom';
  if (value === 5) return 'top';
  return 'scroll';
}

function decimalColor(value: string | undefined) {
  const decimal = Number(value);
  if (!Number.isFinite(decimal)) return '#ffffff';
  const color = Math.max(0, Math.min(0xffffff, Math.round(decimal)));
  return `#${color.toString(16).padStart(6, '0')}`;
}

function scrollDuration(text: string) {
  const visibleLength = Array.from(text).length;
  return clampNumber(6.2 + visibleLength * 0.18, 7.2, 12, 8.5);
}

function cleanDanmakuText(value: string) {
  const stripped = value.replace(/<[^>]+>/g, '').replace(/\s+/g, ' ').trim();
  return decodeEntities(stripped);
}

function decodeEntities(value: string) {
  const textarea = document.createElement('textarea');
  textarea.innerHTML = value;
  return textarea.value.trim();
}

function clampNumber(value: number, min: number, max: number, fallback: number) {
  if (!Number.isFinite(value)) return fallback;
  return Math.min(max, Math.max(min, value));
}
