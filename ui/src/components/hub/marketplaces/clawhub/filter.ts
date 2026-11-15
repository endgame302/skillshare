// Fork-only (endgame302/skillshare): filtering ClawHub's feed on the Hubs page.
import type { ClawHubItem } from '../../../../api/forkClawhub';

export interface ClawHubFilter {
  /** Words that must all appear in the title, id, publisher or description. */
  text: string;
  source: 'all' | 'clawhub' | 'github';
  featuredOnly: boolean;
  /** A trust level, or '' for any. */
  trust: string;
}

export const emptyFilter: ClawHubFilter = { text: '', source: 'all', featuredOnly: false, trust: '' };

/** The items the filter keeps, featured ones first, otherwise in feed order. */
export function filterClawHub(items: ClawHubItem[], f: ClawHubFilter): ClawHubItem[] {
  const words = f.text.toLowerCase().split(/\s+/).filter(Boolean);
  const kept = items.filter((it) => {
    if (f.source !== 'all' && it.source !== f.source) return false;
    if (f.featuredOnly && !it.featured) return false;
    if (f.trust && it.trust !== f.trust) return false;
    if (words.length === 0) return true;
    const hay = `${it.title} ${it.id} ${it.publisher} ${it.description}`.toLowerCase();
    return words.every((w) => hay.includes(w));
  });
  return kept.filter((it) => it.featured).concat(kept.filter((it) => !it.featured));
}

/** Every trust level the feed uses, so the picker never offers a dead option. */
export function trustLevels(items: ClawHubItem[]): string[] {
  return [...new Set(items.map((it) => it.trust).filter(Boolean))].sort();
}

/** The tail of a release folder's path: `clawhub/releases/@owner/slug@version`. */
const releaseTail = /\/clawhub\/releases\/(@[^/]+\/[^/@]+)@([^/]+)$/;

/**
 * The ClawHub version installed for each `@owner/slug`. An install records the folder of the
 * verified release it copied as the skill's source, and that path ends in `@owner/slug@version`.
 * A same-named skill from anywhere else does not count.
 */
export function installedVersions(skills: { source?: string }[], items: ClawHubItem[]): Map<string, string> {
  const listed = new Set(items.map((it) => it.id));
  const found = new Map<string, string>();
  for (const s of skills) {
    const m = s.source?.replaceAll('\\', '/').match(releaseTail);
    if (m && listed.has(m[1])) found.set(m[1], m[2]);
  }
  return found;
}
