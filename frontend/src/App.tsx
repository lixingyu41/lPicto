import { Navigate, Route, Routes, useLocation, type Location } from 'react-router-dom';
import Layout from './components/Layout';
import AlbumsPage from './pages/AlbumsPage';
import FoldersPage from './pages/FoldersPage';
import LibraryPage from './pages/LibraryPage';
import SettingsPage from './pages/SettingsPage';
import ViewerPage from './pages/ViewerPage';

interface ViewerOverlayState {
  backgroundLocation?: Location;
}

export default function App() {
  const location = useLocation();
  const state = location.state as ViewerOverlayState | null;
  const backgroundLocation = state?.backgroundLocation;
  const showingViewerOverlay = Boolean(backgroundLocation && location.pathname.startsWith('/viewer/'));

  return (
    <Layout>
      <Routes location={backgroundLocation ?? location}>
        <Route index element={<Navigate to="/library" replace />} />
        <Route path="/timeline" element={<Navigate to="/library" replace />} />
        <Route path="/library" element={<LibraryPage />} />
        <Route path="/albums" element={<AlbumsPage />} />
        <Route path="/folders" element={<FoldersPage />} />
        <Route path="/settings" element={<SettingsPage />} />
        <Route path="/viewer/:assetId" element={<ViewerPage />} />
      </Routes>
      {showingViewerOverlay && <ViewerPage overlay />}
    </Layout>
  );
}
