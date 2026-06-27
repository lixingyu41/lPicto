const designGridEnabled = true;

export default function DesignGridOverlay() {
  if (!designGridEnabled) return null;
  return <div aria-hidden="true" className="design-grid-overlay" />;
}
