export const routeModules = {
  albums: () => import('../pages/AlbumsPage'),
  collections: () => import('../pages/CollectionsPage'),
  folders: () => import('../pages/FoldersPage'),
  library: () => import('../pages/LibraryPage'),
  settings: () => import('../pages/SettingsPage'),
  viewer: () => import('../pages/ViewerPage'),
};

export type RouteModule = keyof typeof routeModules;

export function preloadRouteModule(route: RouteModule) {
  void routeModules[route]();
}
