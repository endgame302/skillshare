import { apiFetch } from './client';

/**
 * How a plain GitHub repo hub was indexed (fork-only, endgame302/skillshare).
 * Only `plain` is set until the hub's list has been loaded once.
 */
export interface PlainHubInfo {
  plain: boolean;
  repo?: string;
  commit?: string;
  indexedAt?: string;
  listed?: number;
  total?: number;
  capped?: boolean;
}

export const forkPlainHub = {
  info: (hub: string) => apiFetch<PlainHubInfo>(`/fork/plainhub?hub=${encodeURIComponent(hub)}`),
};
