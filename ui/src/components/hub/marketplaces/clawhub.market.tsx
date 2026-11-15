// Fork-only (endgame302/skillshare): registers the ClawHub marketplace on the Hubs page.
import { Store } from 'lucide-react';
import type { Marketplace } from './registry';
import ClawHubView from './clawhub/ClawHubView';

export const marketplace: Marketplace = {
  id: 'clawhub',
  order: 30,
  labelKey: 'clawhub.market.label',
  subKey: 'clawhub.market.sub',
  icon: Store,
  View: ClawHubView,
};
