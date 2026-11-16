// Fork-only (endgame302/skillshare): MCP servers from the official MCP Registry, GitHub's MCP
// registry and Docker's MCP catalog, and plans that turn one into a skillshare MCP server.
import { apiFetch } from './client';
import type { MCPServer } from './mcp';

export type MCPMarketSource = 'official' | 'curated' | 'docker';

export interface MCPMarketEntry {
  name: string;
  title?: string;
  description?: string;
  version?: string;
  namespace?: string;
  /** `active` or `deprecated`. */
  status?: string;
  publishedAt?: string;
  updatedAt?: string;
  repository?: string;
  website?: string;
  /** npm, pypi, oci, nuget, mcpb, cargo, remote, sse, docker, docker-remote, poci… */
  kinds?: string[];
  local?: boolean;
  remote?: boolean;
  installable: boolean;
  /** Why no option can be installed: a refusal code. */
  blocked?: string;
  license?: string;
  stars?: number;
  pushedAt?: string;
  /** A GitHub registry entry the official registry lists too. */
  official?: boolean;
  category?: string;
  pulls?: number;
  image?: string;
  /** A Docker image under mcp/, built and signed by Docker. */
  dockerBuilt?: boolean;
  tags?: string[];
}

export interface MCPMarketStatus {
  /** `error`: nothing to show and the last attempt failed. */
  state: 'ready' | 'loading' | 'error';
  entries: number;
  /** Official: the first crawl has finished; others: loaded. */
  complete: boolean;
  syncing: boolean;
  fetchedAt?: string;
  error?: string;
  retryAt?: string;
  stale: boolean;
}

export interface MCPMarketList {
  source: MCPMarketSource;
  items: MCPMarketEntry[];
  total: number;
  hiddenBulk: number;
  hiddenDeprecated: number;
  bulkThreshold: number;
  status: MCPMarketStatus;
  /** Docker tab only: whether `docker` is on the PATH. */
  dockerFound?: boolean;
}

export interface MCPMarketQuery {
  q?: string;
  kind?: '' | 'local' | 'remote';
  installable?: boolean;
  bulk?: boolean;
  deprecated?: boolean;
  /** name, updated, stars, pulls; empty is relevance while searching. */
  sort?: string;
  offset?: number;
  limit?: number;
  refresh?: boolean;
}

/** A value the user may type: an argument, a non-secret variable or a URL part. */
export interface MCPMarketInput {
  key: string;
  label: string;
  description?: string;
  default?: string;
  format?: string;
  choices?: string[];
  required?: boolean;
  value?: string;
}

/** A variable the server reads from the user's environment. `use`: env, bearer or header:<Name>. */
export interface MCPMarketEnvVar {
  name: string;
  description?: string;
  required?: boolean;
  use: string;
}

export interface MCPMarketWarning {
  code: string;
  params?: Record<string, string>;
}

/** One way to install an entry: a package, a remote endpoint or a Docker image. */
export interface MCPMarketOption {
  id: string;
  kind: string;
  label: string;
  runtime?: string;
  /** Suggested skillshare server name. */
  name: string;
  refused?: string;
  detail?: string;
  /** Set only when nothing is refused and every required input has a value. */
  server?: MCPServer;
  command?: string[];
  url?: string;
  host?: string;
  domain?: string;
  inputs?: MCPMarketInput[];
  missing?: string[];
  envVars?: MCPMarketEnvVar[];
  skipped?: MCPMarketEnvVar[];
  warnings?: MCPMarketWarning[];
}

export interface MCPMarketPlan {
  source: MCPMarketSource;
  entry: MCPMarketEntry;
  options: MCPMarketOption[];
  /** The dashboard machine's OS, as Go names it (windows, darwin, linux). */
  os: string;
  dockerFound: boolean;
}

/** The fork keeps its query keys here rather than in upstream's queryKeys.ts. */
export const mcpMarketKey = ['fork', 'mcpmarket'] as const;

const listPath = (source: MCPMarketSource, q: MCPMarketQuery) => {
  const p = new URLSearchParams({ source });
  if (q.q) p.set('q', q.q);
  if (q.kind) p.set('kind', q.kind);
  if (q.installable) p.set('installable', '1');
  if (q.bulk) p.set('bulk', '1');
  if (q.deprecated) p.set('deprecated', '1');
  if (q.sort) p.set('sort', q.sort);
  if (q.offset) p.set('offset', String(q.offset));
  if (q.limit) p.set('limit', String(q.limit));
  if (q.refresh) p.set('refresh', '1');
  return `/fork/mcpmarket/servers?${p.toString()}`;
};

export const forkMcpMarket = {
  /** One page of a source; the official registry lists what it has indexed so far. */
  list: (source: MCPMarketSource, query: MCPMarketQuery = {}) => apiFetch<MCPMarketList>(listPath(source, query)),
  /** The entry's install options, filled with the values the user typed. Nothing is written. */
  plan: (source: MCPMarketSource, name: string, values: Record<string, string> = {}) =>
    apiFetch<MCPMarketPlan>('/fork/mcpmarket/plan', { method: 'POST', body: JSON.stringify({ source, name, values }) }),
};
