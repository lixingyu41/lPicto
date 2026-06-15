export type ThemeMode = 'system' | 'light' | 'dark';

const themeModeKey = 'lpicto.themeMode';
const mediaQuery = '(prefers-color-scheme: dark)';

export function loadThemeMode(): ThemeMode {
  const raw = localStorage.getItem(themeModeKey);
  return raw === 'light' || raw === 'dark' ? raw : 'system';
}

export function saveThemeMode(mode: ThemeMode) {
  localStorage.setItem(themeModeKey, mode);
  applyThemeMode(mode);
}

export function applyStoredTheme() {
  applyThemeMode(loadThemeMode());
}

export function watchSystemTheme() {
  const media = window.matchMedia(mediaQuery);
  const applyIfSystem = () => {
    if (loadThemeMode() === 'system') {
      applyThemeMode('system');
    }
  };
  const applyCurrent = () => applyThemeMode(loadThemeMode());
  media.addEventListener('change', applyIfSystem);
  window.addEventListener('storage', applyCurrent);
}

function applyThemeMode(mode: ThemeMode) {
  const resolved = mode === 'system' ? systemTheme() : mode;
  document.documentElement.dataset.themeMode = mode;
  document.documentElement.dataset.theme = resolved;
}

function systemTheme(): 'light' | 'dark' {
  return window.matchMedia(mediaQuery).matches ? 'dark' : 'light';
}
