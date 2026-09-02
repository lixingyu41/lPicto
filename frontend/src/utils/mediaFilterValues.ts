import type { LibraryFilterParams, OrientationFilter } from '../types/api';

export type DurationUnit = 'seconds' | 'minutes' | 'hours';

export function rangeInputFromParams(minValue: string | null, maxValue: string | null) {
  const min = positiveNumber(minValue ?? '');
  const max = positiveNumber(maxValue ?? '');
  if (min === undefined && max === undefined) return '';
  if (min !== undefined && max !== undefined && min === max) return String(min);
  return `${min ?? ''}-${max ?? ''}`;
}

export function secondsParamToDurationValue(value: string | null, unit: DurationUnit) {
  const seconds = positiveNumber(value ?? '');
  return seconds === undefined ? '' : formatNumberValue(seconds / durationUnitSeconds(unit));
}

export function bytesParamToMB(value: string | null) {
  const bytes = positiveNumber(value ?? '');
  return bytes === undefined ? '' : formatNumberValue(bytes / (1024 * 1024));
}

export function unixParamToDatetimeLocal(value: string | null) {
  const seconds = positiveNumber(value ?? '');
  if (seconds === undefined) return '';
  const date = new Date(seconds * 1000);
  if (!Number.isFinite(date.getTime())) return '';
  const pad = (part: number) => String(part).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

export function parseResolutionRanges(
  xValue: string,
  yValue: string,
  orientation: OrientationFilter,
): Pick<LibraryFilterParams, 'widthMin' | 'widthMax' | 'heightMin' | 'heightMax' | 'dimensionMode'> {
  const width = parseNumberRange(xValue);
  const height = parseNumberRange(yValue);
  const active = width.min !== undefined || width.max !== undefined || height.min !== undefined || height.max !== undefined;
  return {
    widthMin: width.min,
    widthMax: width.max,
    heightMin: height.min,
    heightMax: height.max,
    dimensionMode: active && orientation === 'all' ? 'both' : undefined,
  };
}

export function parseDurationRange(minValue: string, maxValue: string, unit: DurationUnit): Pick<LibraryFilterParams, 'durationMin' | 'durationMax'> {
  const multiplier = durationUnitSeconds(unit);
  const min = positiveNumber(minValue);
  const max = positiveNumber(maxValue);
  return {
    durationMin: min === undefined ? undefined : roundDurationSeconds(min * multiplier),
    durationMax: max === undefined ? undefined : roundDurationSeconds(max * multiplier),
  };
}

export function parseNumberRange(value: string): { min?: number; max?: number } {
  const clean = value.trim();
  if (clean === '') return {};
  const parts = clean.split('-', 2);
  if (parts.length === 1) {
    const exact = positiveNumber(parts[0]);
    return exact === undefined ? {} : { min: exact, max: exact };
  }
  return { min: positiveNumber(parts[0]), max: positiveNumber(parts[1]) };
}

export function positiveNumber(value: string): number | undefined {
  const clean = value.trim();
  if (clean === '') return undefined;
  const parsed = Number(clean);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : undefined;
}

export function datetimeLocalToUnix(value: string): number | undefined {
  if (!value) return undefined;
  const parsed = new Date(value).getTime();
  return Number.isFinite(parsed) ? Math.floor(parsed / 1000) : undefined;
}

export function mbToBytes(value: string): number | undefined {
  const parsed = positiveNumber(value);
  return parsed === undefined ? undefined : Math.round(parsed * 1024 * 1024);
}

export function formatNumberValue(value: number): string {
  if (!Number.isFinite(value)) return '';
  return Number.isInteger(value) ? String(value) : String(Number(value.toFixed(9)));
}

export function parseDurationUnit(value: unknown): DurationUnit {
  return value === 'seconds' || value === 'hours' ? value : 'minutes';
}

export function durationUnitSeconds(unit: DurationUnit) {
  return unit === 'seconds' ? 1 : unit === 'hours' ? 3600 : 60;
}

export function convertDurationValue(value: string, from: DurationUnit, to: DurationUnit) {
  const parsed = positiveNumber(value);
  if (parsed === undefined) return value.trim() === '' ? '' : value;
  return formatNumberValue((parsed * durationUnitSeconds(from)) / durationUnitSeconds(to));
}

function roundDurationSeconds(value: number) {
  return Math.round(value * 1000) / 1000;
}
