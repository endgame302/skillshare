// Fork-only (endgame302/skillshare): registers the Agents marketplace on the Hubs page.
import { Bot } from 'lucide-react';
import type { Marketplace } from './registry';
import AgentsView from './agents/AgentsView';

export const marketplace: Marketplace = {
  id: 'agents',
  order: 40,
  labelKey: 'agentsMarket.market.label',
  subKey: 'agentsMarket.market.sub',
  icon: Bot,
  View: AgentsView,
};
