export const settingsSections = [
  { id: 'libraries', label: '图库', slug: 'libraries' },
  { id: 'cache', label: '缓存', slug: 'cache' },
  { id: 'viewer', label: '查看与播放', slug: 'viewer' },
  { id: 'appearance', label: '外观', slug: 'appearance' },
  { id: 'ai', label: 'AI', slug: 'ai' },
  { id: 'tasks', label: '任务', slug: 'tasks' },
] as const;

export type SettingsSectionId = (typeof settingsSections)[number]['id'];

const settingsSectionStorageKey = 'lpicto.settings.section';
const defaultSettingsSection: SettingsSectionId = 'libraries';

export function settingsSectionFromSlug(value: string | undefined): SettingsSectionId | null {
  const normalized = value?.trim().toLowerCase();
  if (normalized === 'video-proxy') return 'cache';
  return settingsSections.find((section) => section.slug === normalized)?.id ?? null;
}

export function settingsSectionPath(id: SettingsSectionId): string {
  const section = settingsSections.find((item) => item.id === id);
  return `/settings/${section?.slug ?? settingsSections[0].slug}`;
}

export function loadSettingsSection(): SettingsSectionId {
  try {
    const stored = window.localStorage.getItem(settingsSectionStorageKey) ?? undefined;
    if (stored === 'videoProxy') return 'cache';
    return settingsSections.some((section) => section.id === stored) ? stored as SettingsSectionId : defaultSettingsSection;
  } catch {
    return defaultSettingsSection;
  }
}

export function saveSettingsSection(id: SettingsSectionId) {
  try {
    window.localStorage.setItem(settingsSectionStorageKey, id);
  } catch {
    // The route remains the source of truth when browser storage is unavailable.
  }
}
