import { useEffect } from 'react';
import { Navigate, Route, Routes, useLocation, useNavigate, type Location } from 'react-router-dom';
import Layout from './components/Layout';
import AlbumsPage from './pages/AlbumsPage';
import CollectionsPage from './pages/CollectionsPage';
import FoldersPage from './pages/FoldersPage';
import LibraryPage from './pages/LibraryPage';
import SettingsPage from './pages/SettingsPage';
import ViewerPage from './pages/ViewerPage';
import { emitViewerOverlayCloseCompleted, viewerOverlayCloseRequested } from './utils/pageState';

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
    <Layout routeLocation={routeLocation} overlay={showingViewerOverlay ? <ViewerPage overlay /> : null}>
      <Routes location={routeLocation}>
        <Route index element={<Navigate to="/library" replace />} />
        <Route path="/timeline" element={<Navigate to="/library" replace />} />
        <Route path="/library" element={<LibraryPage />} />
        <Route path="/albums" element={<AlbumsPage />} />
        <Route path="/folders" element={<FoldersPage />} />
        <Route path="/collections" element={<CollectionsPage />} />
        <Route path="/settings" element={<SettingsPage />} />
        <Route path="/settings/:section" element={<SettingsPage />} />
        <Route path="/viewer/:assetId" element={<ViewerPage />} />
      </Routes>
    </Layout>
  );
}
