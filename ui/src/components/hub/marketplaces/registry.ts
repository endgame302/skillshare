import type { ComponentType } from 'react';
import type { LucideIcon } from 'lucide-react';

/**
 * A place to find skills that is not a hub index: GitHub search, skills.sh, ClawHub (fork-only,
 * endgame302/skillshare). The Hubs page lists them under "Marketplaces" and shows `View` in its
 * right-hand pane when one is picked.
 */
export interface Marketplace {
  /** Stable id; the page selects it as `market:<id>`. */
  id: string;
  /** Position in the list, lowest first. */
  order: number;
  /** Translation keys for the list row (`fork/*.json` in `src/i18n`). */
  labelKey: string;
  subKey: string;
  icon: LucideIcon;
  View: ComponentType;
}

export const MARKET = 'market:';

// Each marketplace registers itself in its own `<id>.market.tsx` next to this file, exporting
// `marketplace`. They are collected here so no two features edit the same line.
const modules = import.meta.glob<Marketplace>('./*.market.tsx', { eager: true, import: 'marketplace' });

export const MARKETPLACES: Marketplace[] = Object.values(modules).sort((a, b) => a.order - b.order);

export const findMarketplace = (key: string | null) =>
  key?.startsWith(MARKET) ? MARKETPLACES.find((m) => MARKET + m.id === key) : undefined;
