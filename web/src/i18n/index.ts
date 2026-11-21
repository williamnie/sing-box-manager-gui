import { useSyncExternalStore } from 'react';
import { en } from './en';

export type Locale = 'zh-CN' | 'en';

export const localeOptions: { key: Locale; label: string }[] = [
  { key: 'zh-CN', label: '简体中文' },
  { key: 'en', label: 'English' },
];

const STORAGE_KEY = 'sbm-locale';
const listeners = new Set<() => void>();

function readSaved(): Locale | null {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (saved === 'zh-CN' || saved === 'en') return saved;
  } catch {
    // localStorage can be unavailable (privacy mode); fall back to detection.
  }
  return null;
}

function detect(): Locale {
  const saved = readSaved();
  if (saved) return saved;
  const language =
    (typeof navigator !== 'undefined' && (navigator.language || (navigator as { userLanguage?: string }).userLanguage)) || 'en-US';
  return language.toLowerCase().startsWith('zh') ? 'zh-CN' : 'en';
}

let current: Locale = detect();

function applyDocumentLocale(locale: Locale) {
  if (typeof document !== 'undefined') {
    document.documentElement.lang = locale;
  }
}

applyDocumentLocale(current);

export function getLocale(): Locale {
  return current;
}

export function setLocale(locale: Locale) {
  if (locale === current) return;
  current = locale;
  applyDocumentLocale(locale);
  try {
    localStorage.setItem(STORAGE_KEY, locale);
  } catch {
    // Ignore storage failures; the choice still applies for this session.
  }
  listeners.forEach((listener) => listener());
}

export function subscribeLocale(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function useLocale(): Locale {
  return useSyncExternalStore(subscribeLocale, getLocale);
}

/**
 * Translate a message. `message` is the Chinese source text; the English
 * dictionary maps it to English. Unknown messages fall back to the source.
 * Placeholders use `{name}` and are filled from `params`.
 */
export function t(message: string, params?: Record<string, string | number | boolean | null | undefined>): string {
  let text = current === 'zh-CN' ? message : en[message] ?? message;
  if (params) {
    for (const [key, value] of Object.entries(params)) {
      text = text.split(`{${key}}`).join(value === null || value === undefined ? '' : String(value));
    }
  }
  return text;
}
