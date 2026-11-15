// Fork-only (endgame302/skillshare): ClawHub's skills feed and installs from it.
import { apiFetch } from './client';

/** Why an entry cannot be installed here. */
export type ClawHubBlocked = 'github' | 'unavailable' | 'no_integrity' | 'unsupported';

export interface ClawHubItem {
  /** `@owner/slug` */
  id: string;
  owner: string;
  slug: string;
  title: string;
  description: string;
  version: string;
  featured: boolean;
  publisher: string;
  /** How far ClawHub vouches for the publisher, e.g. `official`. */
  trust: string;
  /** Where the files come from: a ClawHub-hosted release, or a GitHub folder at a pinned commit. */
  source: 'clawhub' | 'github';
  github?: { repo: string; path: string; commit: string; contentHash: string };
  /** `sha256:<hex>` of a hosted release. */
  integrity?: string;
  installable: boolean;
  blocked?: ClawHubBlocked;
}

export interface ClawHubFeed {
  items: ClawHubItem[];
  generatedAt: string;
  expiresAt: string;
  fetchedAt: string;
  registry: string;
  /** Set when an expired copy is shown because ClawHub could not be reached. */
  stale: boolean;
  staleReason?: string;
}

export interface ClawHubInstallResult {
  skillName: string;
  action: string;
  warnings?: string[];
  /** `clawhub://@owner/slug@version` */
  origin: string;
  version: string;
}

/** The query key of the feed; the fork keeps it here rather than in upstream's queryKeys.ts. */
export const clawhubFeedKey = ['fork', 'clawhub', 'feed'] as const;

export const forkClawhub = {
  /** The cached feed; `refresh` fetches it from ClawHub again. */
  feed: (refresh = false) => apiFetch<ClawHubFeed>(`/fork/clawhub/feed${refresh ? '?refresh=1' : ''}`),
  /** The server downloads the release its cached feed lists and checks it against the feed's hash. */
  install: (id: string, version: string, force = false) =>
    apiFetch<ClawHubInstallResult>('/fork/clawhub/install', { method: 'POST', body: JSON.stringify({ id, version, force }) }),
};
