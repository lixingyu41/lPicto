import React from 'react';
import ReactDOM from 'react-dom/client';
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom';
import App from './App';
import LibraryPage from './pages/LibraryPage';
import FoldersPage from './pages/FoldersPage';
import AlbumsPage from './pages/AlbumsPage';
import ViewerPage from './pages/ViewerPage';
import SettingsPage from './pages/SettingsPage';
import './styles/global.css';

ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
  <React.StrictMode>
    <BrowserRouter>
      <Routes>
        <Route element={<App />}>
          <Route index element={<Navigate to="/library" replace />} />
          <Route path="/timeline" element={<Navigate to="/library" replace />} />
          <Route path="/library" element={<LibraryPage />} />
          <Route path="/albums" element={<AlbumsPage />} />
          <Route path="/folders" element={<FoldersPage />} />
          <Route path="/settings" element={<SettingsPage />} />
          <Route path="/viewer/:assetId" element={<ViewerPage />} />
        </Route>
      </Routes>
    </BrowserRouter>
  </React.StrictMode>,
);
