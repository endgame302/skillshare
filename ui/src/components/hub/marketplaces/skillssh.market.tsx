// Fork-only (endgame302/skillshare): registers skills.sh in the Hubs page's Marketplaces list.
import { TrendingUp } from 'lucide-react';
import type { Marketplace } from './registry';
import SkillsshView from './skillssh/SkillsshView';

export const marketplace: Marketplace = {
  id: 'skillssh',
  order: 20,
  labelKey: 'hubs.skillssh.label',
  subKey: 'hubs.skillssh.sub',
  icon: TrendingUp,
  View: SkillsshView,
};
