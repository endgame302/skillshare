// Fork-only (endgame302/skillshare): Claude Code plugin catalogs and the hand-off to Claude's own CLI.
import { apiFetch } from './client';

export type PluginCatalogId = 'official' | 'community' | 'wshobson';

/** An entry's source as the catalog writes it. `kind` is `path` for files inside the catalog repo. */
export interface CatalogSource {
  kind: string;
  path?: string;
  url?: string;
  repo?: string;
  ref?: string;
  /** Set only when the catalog pins a full commit. */
  sha?: string;
  package?: string;
  version?: string;
  command?: string;
}

/**
 * How an entry installs: `add` through skillshare's plugin add, pinned to the catalog commit; `native` through
 * Claude Code's CLI, then imported; `manual` only inside Claude Code; `none` not at all (`problem` says why).
 */
export type CatalogInstall = 'add' | 'native' | 'manual' | 'none';

export interface CatalogEntry {
  name: string;
  displayName?: string;
  description: string;
  category?: string;
  author?: string;
  homepage?: string;
  version?: string;
  keywords?: string[];
  source: CatalogSource;
  install: CatalogInstall;
  /** add: always, the catalog commit pins it; native: the catalog pins a commit (`source.sha`). */
  pinned: boolean;
  problem?: string;
}

export interface PluginCatalog {
  id: string;
  repo: string;
  /** The catalog's own name: Claude Code's marketplace name. */
  market: string;
  /** The catalog repo commit the list was read at. */
  commit: string;
  entries: CatalogEntry[];
  renames?: Record<string, string>;
  fetchedAt: string;
  stale: boolean;
  staleReason?: string;
}

/** One claude command; `args` is the argv after `claude`. */
export interface HandoffStep {
  name: 'add' | 'update' | 'install';
  args: string[];
}

export interface HandoffPlan {
  /** Where claude is; '' when it is not on PATH, and then `steps` is empty. */
  claude: string;
  claudeVersion?: string;
  market: string;
  /** `NAME@MARKET` */
  pluginId: string;
  /** Claude Code already has this marketplace from this repo: it is updated, not added. */
  registered: boolean;
  steps: HandoffStep[];
  /** The skillshare import command, shown to the user. */
  import: string;
}

export interface HandoffResult {
  pluginId: string;
  message?: string;
}

export interface HandoffRequest {
  catalog: string;
  name: string;
  /** The catalog commit the list showed; the server refuses when its copy moved. */
  commit: string;
  /** The entry's pinned commit as the list showed it, '' when none. */
  sha: string;
  /** true asks only for the plan, running nothing that changes Claude Code. */
  dryRun: boolean;
}

/** The query key of one catalog; the fork keeps it here rather than in upstream's queryKeys.ts. */
export const pluginCatalogKey = (id: string) => ['fork', 'plugins', 'catalog', id] as const;

export const forkPlugins = {
  /** The cached catalog; `refresh` reads it from GitHub again. */
  catalog: (id: string, refresh = false) =>
    apiFetch<PluginCatalog>(`/fork/plugins/catalog?id=${encodeURIComponent(id)}${refresh ? '&refresh=1' : ''}`),
  /** Plans (dryRun) or runs the Claude Code install of an entry hosted outside its catalog. */
  handoff: (body: HandoffRequest) =>
    apiFetch<{ plan: HandoffPlan; result?: HandoffResult }>('/fork/plugins/handoff', { method: 'POST', body: JSON.stringify(body) }),
};
