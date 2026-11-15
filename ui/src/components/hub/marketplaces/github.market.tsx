// Fork-only (endgame302/skillshare): GitHub search, listed under Marketplaces on the Hubs page.
import { Github } from 'lucide-react';
import GithubMarket from './GithubMarket';
import type { Marketplace } from './registry';

export const marketplace: Marketplace = {
  id: 'github',
  order: 10,
  labelKey: 'hubs.market.github.label',
  subKey: 'hubs.market.github.sub',
  icon: Github,
  View: GithubMarket,
};
