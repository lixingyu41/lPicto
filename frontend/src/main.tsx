import React from 'react';
import ReactDOM from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import App from './App';
import { api, configureMediaOrigins } from './api/client';
import { applyStoredTheme, watchSystemTheme } from './utils/themePrefs';
import { preloadAITagTree } from './utils/aiTagTreeCache';
import './styles/global.css';

applyStoredTheme();
watchSystemTheme();

if ('serviceWorker' in navigator && window.isSecureContext) {
  window.addEventListener('load', () => {
    void navigator.serviceWorker.register('/service-worker.js').catch(() => undefined);
  });
}

async function startApplication() {
  try {
    const config = await api.publicConfig();
    configureMediaOrigins(config.mediaOriginPorts);
  } catch {
    configureMediaOrigins([]);
  }
  ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
    <React.StrictMode>
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </React.StrictMode>,
  );
  const preload = () => void preloadAITagTree();
  const requestIdle = (window as Window & { requestIdleCallback?: (callback: IdleRequestCallback, options?: IdleRequestOptions) => number }).requestIdleCallback;
  if (requestIdle) {
    requestIdle(preload, { timeout: 2_000 });
  } else {
    globalThis.setTimeout(preload, 1_000);
  }
}

void startApplication();
