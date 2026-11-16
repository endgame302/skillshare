// Fork-only (endgame302/skillshare): community subagent directories and installs from them.
import { apiFetch } from './client';

export type AgentSource = 'voltagent' | 'wshobson' | 'aitmpl';

export interface AgentScan {
  tool: string;
  toolVersion?: string;
  severity: string;
  score?: number;
  scannedAt?: string;
}

export interface MarketAgent {
  id: string;
  name: string;
  title: string;
  description: string;
  category: string;
  repo: string;
  dir: string;
  file: string;
  tools?: string[];
  model?: string;
  downloads?: number;
  scan?: AgentScan;
  pageUrl?: string;
}

export interface AgentListing {
  source: string;
  repo: string;
  commit?: string;
  generatedAt?: string;
  agents: MarketAgent[];
  fetchedAt: string;
  stale: boolean;
  staleReason?: string;
}

export const agentListingKey = (source: string) => ['fork', 'agents', 'listing', source] as const;

export const agentInstallSource = (a: MarketAgent) => `github.com/${a.repo}/${a.dir}`;

export const forkAgents = {
  listing: (source: string, refresh = false) =>
    apiFetch<AgentListing>(`/fork/agents/listing?source=${encodeURIComponent(source)}${refresh ? '&refresh=1' : ''}`),
};
