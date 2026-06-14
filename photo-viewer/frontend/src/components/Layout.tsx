import type { ReactNode } from 'react';
import Sidebar from './Sidebar';
import { SidebarPanelProvider } from './SidebarContext';

interface Props {
  children: ReactNode;
}

export default function Layout({ children }: Props) {
  return (
    <SidebarPanelProvider>
      <div className="app-shell">
        <aside className="sidebar">
          <Sidebar />
        </aside>
        <main className="main-panel">{children}</main>
      </div>
    </SidebarPanelProvider>
  );
}
