// Fork-only (endgame302/skillshare): pure helpers of the Plugins marketplace view and its dialogs.
import { ApiError } from '../../../../api/client';
import type { CatalogEntry } from '../../../../api/forkPlugins';
import type { PluginInventory } from '../../../../api/plugins';
import type { useT } from '../../../../i18n';

type T = ReturnType<typeof useT>;

/** Whether skillshare manages this plugin already: a package of that name, or one bound to `name@market`. */
export function isInstalled(inventory: PluginInventory | undefined, entry: CatalogEntry, market: string) {
  if (!inventory) return false;
  if (inventory.packages[entry.name]) return true;
  const id = `${entry.name}@${market}`;
  return Object.values(inventory.packages).some((p) => Object.values(p.bindings).some((b) => b?.id === id));
}

/** `needle` is already lower case; an empty one matches everything. */
export function matchesText(e: CatalogEntry, needle: string) {
  if (!needle) return true;
  return [e.name, e.displayName, e.description, e.category, e.author, ...(e.keywords ?? [])]
    .some((v) => v?.toLowerCase().includes(needle));
}

export const safeHomepage = (url?: string) => (url && /^https?:\/\//i.test(url) ? url : undefined);

export function catalogErrorText(t: T, err: unknown): string {
  if (err instanceof ApiError && err.code === 'plugins_market.rate_limited') return t('pluginsMarket.error.rateLimited');
  return t('pluginsMarket.error.load', { message: err instanceof Error ? err.message : String(err) });
}

// The server's hand-off error codes, as literal keys of their sentences.
const HANDOFF_ERRORS: Record<string, string> = {
  'plugins_market.catalog_moved': 'pluginsMarket.handoff.error.moved',
  'plugins_market.catalog_changed': 'pluginsMarket.handoff.error.moved',
  'plugins_market.not_found': 'pluginsMarket.handoff.error.moved',
  'plugins_market.market_conflict': 'pluginsMarket.handoff.error.conflict',
  'plugins_market.claude_failed': 'pluginsMarket.handoff.error.claude',
  'plugins_market.claude_missing': 'pluginsMarket.handoff.error.missing',
  'plugins_market.busy': 'pluginsMarket.handoff.error.busy',
  'plugins_market.project_mode': 'pluginsMarket.handoff.error.projectMode',
};

export function handoffErrorText(t: T, err: unknown): string {
  const message = err instanceof Error ? err.message : String(err);
  if (!(err instanceof ApiError) || !err.code || !HANDOFF_ERRORS[err.code]) return message;
  return t(HANDOFF_ERRORS[err.code], { message, market: String(err.params?.market ?? '') });
}

/** One argv as a shell line; an argument with a space, quote or backslash is quoted. */
export const commandLine = (args: string[]) =>
  ['claude', ...args.map((a) => (/^[\w@%+=:,./-]+$/.test(a) ? a : `"${a.replace(/"/g, '\\"')}"`))].join(' ');
