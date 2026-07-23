import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useId,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from 'react';
import { createPortal } from 'react-dom';

export interface CompactSidebarMenuOption {
  active?: boolean;
  closeOnSelect?: boolean;
  disabled?: boolean;
  icon: ReactNode;
  key: string;
  label: string;
  onSelect: () => void;
  trailing?: ReactNode;
}

interface CompactSidebarMenuGroupValue {
  activeId: string | null;
  closeMenu: (restoreFocus?: boolean) => void;
  openMenu: (id: string, trigger: HTMLButtonElement | null, persistent?: boolean) => void;
  panelHostRef: React.RefObject<HTMLDivElement>;
  toggleMenu: (id: string, trigger: HTMLButtonElement | null, persistent?: boolean) => void;
}

const CompactSidebarMenuGroupContext = createContext<CompactSidebarMenuGroupValue | null>(null);

export function CompactSidebarMenuGroup({ children }: { children: ReactNode }) {
  const groupRef = useRef<HTMLDivElement>(null);
  const panelHostRef = useRef<HTMLDivElement>(null);
  const activeTriggerRef = useRef<HTMLButtonElement | null>(null);
  const persistentMenuRef = useRef(false);
  const [activeId, setActiveId] = useState<string | null>(null);

  const closeMenu = useCallback((restoreFocus = false) => {
    if (persistentMenuRef.current) return;
    setActiveId(null);
    if (restoreFocus && activeTriggerRef.current) {
      window.requestAnimationFrame(() => activeTriggerRef.current?.focus());
    }
  }, []);

  const openMenu = useCallback((id: string, trigger: HTMLButtonElement | null, persistent = false) => {
    setActiveId((current) => {
      if (current && current !== id && persistentMenuRef.current) return current;
      activeTriggerRef.current = trigger;
      persistentMenuRef.current = persistent;
      return id;
    });
  }, []);

  const toggleMenu = useCallback((id: string, trigger: HTMLButtonElement | null, persistent = false) => {
    setActiveId((current) => {
      if (current === id) {
        persistentMenuRef.current = false;
        return null;
      }
      if (current && persistentMenuRef.current) return current;
      activeTriggerRef.current = trigger;
      persistentMenuRef.current = persistent;
      return id;
    });
  }, []);

  useEffect(() => {
    if (!activeId) return undefined;
    const closeFromOutside = (event: PointerEvent) => {
      if (!groupRef.current?.contains(event.target as Node)) closeMenu();
    };
    document.addEventListener('pointerdown', closeFromOutside, true);
    return () => document.removeEventListener('pointerdown', closeFromOutside, true);
  }, [activeId, closeMenu]);

  return (
    <CompactSidebarMenuGroupContext.Provider value={{ activeId, closeMenu, openMenu, panelHostRef, toggleMenu }}>
      <div className="sidebar-filter-icon-group" ref={groupRef}>
        {children}
        <div className="sidebar-compact-menu-panel-host" ref={panelHostRef} />
      </div>
    </CompactSidebarMenuGroupContext.Provider>
  );
}

export function CompactSidebarMenu({
  ariaLabel,
  footer,
  options,
  persistent = false,
  title,
  trigger,
}: {
  ariaLabel: string;
  footer?: ReactNode;
  options: CompactSidebarMenuOption[];
  persistent?: boolean;
  title: string;
  trigger: ReactNode;
}) {
  const menuId = useId();
  const triggerRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const group = useContext(CompactSidebarMenuGroupContext);
  const open = group?.activeId === menuId;

  const focusOption = (index: number) => {
    const buttons = panelRef.current?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)');
    if (!buttons?.length) return;
    buttons[(index + buttons.length) % buttons.length].focus();
  };

  const handleTriggerKeyDown = (event: KeyboardEvent<HTMLButtonElement>) => {
    if (event.key === 'Escape') {
      event.preventDefault();
      if (!persistent) group?.closeMenu(true);
      return;
    }
    if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return;
    event.preventDefault();
    group?.openMenu(menuId, triggerRef.current, persistent);
    window.requestAnimationFrame(() => focusOption(event.key === 'ArrowDown' ? 0 : -1));
  };

  const handlePanelKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'Escape') {
      event.preventDefault();
      if (!persistent) group?.closeMenu(true);
      return;
    }
    if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return;
    event.preventDefault();
    const buttons = Array.from(panelRef.current?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') ?? []);
    const currentIndex = buttons.indexOf(document.activeElement as HTMLButtonElement);
    if (event.key === 'Home') focusOption(0);
    else if (event.key === 'End') focusOption(-1);
    else focusOption(currentIndex + (event.key === 'ArrowDown' ? 1 : -1));
  };

  const panel = open && group?.panelHostRef.current ? createPortal(
    <div
      aria-label={ariaLabel}
      className="sidebar-compact-menu-panel"
      id={menuId}
      ref={panelRef}
      role="listbox"
      onKeyDown={handlePanelKeyDown}
    >
      {options.map((option) => (
        <button
          aria-selected={option.active}
          className={option.active ? 'active' : ''}
          disabled={option.disabled}
          key={option.key}
          role="option"
          type="button"
          onClick={() => {
            option.onSelect();
            if (!persistent && option.closeOnSelect === true) group.closeMenu();
          }}
        >
          {option.icon}
          <span>{option.label}</span>
          {option.trailing}
        </button>
      ))}
      {footer}
    </div>,
    group.panelHostRef.current,
  ) : null;

  return (
    <>
      <button
        aria-controls={open ? menuId : undefined}
        aria-expanded={open}
        aria-haspopup="listbox"
        aria-label={ariaLabel}
        className="sidebar-compact-trigger"
        ref={triggerRef}
        title={title}
        type="button"
        onClick={() => group?.toggleMenu(menuId, triggerRef.current, persistent)}
        onKeyDown={handleTriggerKeyDown}
      >
        {trigger}
      </button>
      {panel}
    </>
  );
}
