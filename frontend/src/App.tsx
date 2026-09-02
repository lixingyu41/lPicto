import { lazy, Suspense, useEffect } from 'react';
import { Navigate, Route, Routes, useLocation, useNavigate, type Location } from 'react-router-dom';
import Layout from './components/Layout';
import { emitViewerOverlayCloseCompleted, viewerOverlayCloseRequested } from './utils/pageState';
import { routeModules } from './utils/routeModules';

const AlbumsPage = lazy(routeModules.albums);
const CollectionsPage = lazy(routeModules.collections);
const FoldersPage = lazy(routeModules.folders);
const LibraryPage = lazy(routeModules.library);
const SettingsPage = lazy(routeModules.settings);
const ViewerPage = lazy(routeModules.viewer);

interface ViewerOverlayState {
  backgroundLocation?: Location;
}

export default function App() {
  const location = useLocation();
  const navigate = useNavigate();
  const state = location.state as ViewerOverlayState | null;
  const backgroundLocation = state?.backgroundLocation;
  const routeLocation = backgroundLocation ?? location;
  const showingViewerOverlay = Boolean(backgroundLocation && location.pathname.startsWith('/viewer/'));

  useEffect(() => {
    const handleViewerOverlayClose = (event: Event) => {
      if (!showingViewerOverlay || !backgroundLocation) return;
      if (event instanceof CustomEvent && event.detail && typeof event.detail === 'object') {
        (event.detail as { handled?: boolean }).handled = true;
      }
      navigate(
        {
          pathname: backgroundLocation.pathname,
          search: backgroundLocation.search,
          hash: backgroundLocation.hash,
        },
        { replace: true, state: null },
      );
      window.setTimeout(emitViewerOverlayCloseCompleted, 0);
    };
    window.addEventListener(viewerOverlayCloseRequested, handleViewerOverlayClose);
    return () => window.removeEventListener(viewerOverlayCloseRequested, handleViewerOverlayClose);
  }, [backgroundLocation, navigate, showingViewerOverlay]);

  return (
    <Layout
      routeLocation={routeLocation}
      overlay={showingViewerOverlay ? <Suspense fallback={null}><ViewerPage overlay /></Suspense> : null}
    >
      <Suspense fallback={null}>
        <Routes location={routeLocation}>
          <Route index element={<Navigate to="/library" replace />} />
          <Route path="/timeline" element={<Navigate to="/library" replace />} />
          <Route path="/library" element={<LibraryPage key="library" />} />
          <Route path="/recent" element={<LibraryPage key="recent" mode="recent" />} />
          <Route path="/albums" element={<AlbumsPage />} />
          <Route path="/folders" element={<FoldersPage />} />
          <Route path="/collections" element={<CollectionsPage />} />
          <Route path="/settings" element={<SettingsPage />} />
          <Route path="/settings/:section" element={<SettingsPage />} />
          <Route path="/viewer/:assetId" element={<ViewerPage />} />
        </Routes>
      </Suspense>
    </Layout>
  );
}
