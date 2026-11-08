export type Theme = 'light' | 'dark';
const storageKey = 'sessionsplit-theme';

export function readTheme(): Theme {
  try {
    const saved = localStorage.getItem(storageKey);
    if (saved === 'light' || saved === 'dark') return saved;
  } catch { /* Use system preference when storage is unavailable. */ }
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

export function applyTheme(theme: Theme, persist = false) {
  document.documentElement.dataset.theme = theme;
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', theme === 'dark' ? '#212529' : '#f8f9fa');
  if (persist) {
    try { localStorage.setItem(storageKey, theme); }
    catch { /* The selected theme still applies for this session. */ }
  }
}
