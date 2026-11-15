import type { Locale, Messages } from './locales';

/**
 * Fork-only strings (endgame302/skillshare). Each fork feature keeps its strings in its own
 * `fork/<feature>.json` as `{ "en": { "key": "text" }, "de": { ... } }`, so the fork never edits
 * upstream's locale files and its rebases do not conflict there. A locale a file leaves out
 * falls back to the file's English, which keeps every locale in parity with English.
 */
type FeatureMessages = Partial<Record<Locale, Messages>> & { en: Messages };

const files = import.meta.glob<FeatureMessages>('./fork/*.json', { eager: true, import: 'default' });

export function withForkMessages(base: Record<Locale, Messages>): Record<Locale, Messages> {
  const merged = { ...base };
  for (const locale of Object.keys(base) as Locale[]) {
    merged[locale] = { ...base[locale] };
    for (const file of Object.values(files)) Object.assign(merged[locale], file.en, file[locale]);
  }
  return merged;
}
