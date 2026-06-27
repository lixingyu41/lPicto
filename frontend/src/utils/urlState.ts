import type { NavigateFunction, Location } from 'react-router-dom';

type URLStateValue = string | number | boolean | null | undefined;

export function replaceURLState(
  navigate: NavigateFunction,
  location: Location,
  values: Record<string, URLStateValue>,
  ownedKeys: string[],
) {
  const currentPath = currentURLParts(location);
  const nextSearch = buildURLStateSearch(currentPath.search, values, ownedKeys);
  const current = `${currentPath.pathname}${currentPath.search}${currentPath.hash}`;
  const next = `${currentPath.pathname}${nextSearch}${currentPath.hash}`;
  if (next === current) return;
  if (typeof window !== 'undefined') {
    window.history.replaceState(window.history.state, '', next);
    return;
  }
  navigate(next, { replace: true, state: location.state });
}

export function currentURLPath(location: Location) {
  const current = currentURLParts(location);
  return `${current.pathname}${current.search}`;
}

export function currentURLLocation(location: Location): Location {
  const current = currentURLParts(location);
  return {
    ...location,
    hash: current.hash,
    pathname: current.pathname,
    search: current.search,
  };
}

export function currentURLHasParam(location: Location, key: string) {
  return new URLSearchParams(currentURLParts(location).search).has(key);
}

function currentURLParts(location: Location) {
  if (typeof window === 'undefined') {
    return {
      hash: location.hash,
      pathname: location.pathname,
      search: location.search,
    };
  }
  return {
    hash: window.location.hash,
    pathname: window.location.pathname,
    search: window.location.search,
  };
}

export function buildURLStateSearch(currentSearch: string, values: Record<string, URLStateValue>, ownedKeys: string[]) {
  const params = new URLSearchParams(currentSearch);
  ownedKeys.forEach((key) => params.delete(key));
  Object.entries(values).forEach(([key, value]) => {
    if (value === undefined || value === null || value === '') return;
    params.set(key, String(value));
  });
  const text = params.toString();
  return text ? `?${text}` : '';
}

export function positiveIntParam(value: string | null) {
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed > 0 ? parsed : null;
}

export function nonNegativeIntParam(value: string | null) {
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed >= 0 ? parsed : null;
}

export function booleanParam(value: string | null, fallback: boolean) {
  const normalized = value?.trim().toLowerCase();
  if (normalized === '1' || normalized === 'true' || normalized === 'yes') return true;
  if (normalized === '0' || normalized === 'false' || normalized === 'no') return false;
  return fallback;
}
