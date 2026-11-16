// Fork-only (endgame302/skillshare): registers the Plugins marketplace on the Hubs page.
import { Puzzle } from 'lucide-react';
import type { Marketplace } from './registry';
import PluginsMarketView from './plugins/PluginsMarketView';

export const marketplace: Marketplace = {
  id: 'plugins',
  order: 50,
  labelKey: 'pluginsMarket.market.label',
  subKey: 'pluginsMarket.market.sub',
  icon: Puzzle,
  View: PluginsMarketView,
};
