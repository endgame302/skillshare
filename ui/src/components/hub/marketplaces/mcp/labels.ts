// Fork-only (endgame302/skillshare): MCP marketplace strings chosen at run time.
import { ApiError } from '../../../../api/client';
import type { MCPMarketWarning } from '../../../../api/forkMcpMarket';

type T = (key: string, params?: Record<string, string | number>) => string;

/** Refusal codes the server sends; an unknown one shows the server's own words. */
const refusals = new Set([
  'sse', 'mcpb', 'cargo', 'unsupported_package', 'unsupported_transport', 'package_http', 'no_version', 'custom_registry',
  'smithery', 'insecure_http', 'bad_url', 'secret_literal', 'secret_arg', 'secret_mixed', 'docker_volumes', 'docker_template',
  'poci', 'no_install_data', 'invalid',
]);

/** Warning codes the server sends; an unknown one shows its code. */
const warnings = new Set([
  'remote', 'domain_mismatch', 'broad_token', 'first_run', 'tag', 'repo_mismatch', 'docker_self_built', 'oauth', 'allow_hosts',
  'deprecated', 'bulk_publisher', 'docker_missing', 'not_in_official',
]);

const kinds: Record<string, string> = {
  npm: 'npm', pypi: 'PyPI', oci: 'OCI', nuget: 'NuGet', mcpb: 'MCPB', cargo: 'Cargo', sse: 'SSE', docker: 'Docker', poci: 'poci',
};

/** How an install option runs, for a tag; '' hides it. */
export function kindLabel(t: T, kind: string): string {
  if (kind === 'remote' || kind === 'docker-remote') return t('mcpmarket.kind.remote');
  if (kind === 'none') return '';
  return kinds[kind] ?? kind;
}

export function refusalText(t: T, code: string, detail?: string): string {
  return refusals.has(code) ? t(`mcpmarket.refuse.${code}`, { detail: detail ?? '' }) : detail || code;
}

export function warningText(t: T, w: MCPMarketWarning): string {
  return warnings.has(w.code) ? t(`mcpmarket.warn.${w.code}`, w.params) : w.code;
}

export function mcpMarketErrorText(t: T, err: unknown): string {
  if (!(err instanceof ApiError)) return (err as Error)?.message ?? String(err);
  switch (err.code) {
    case 'mcpmarket_unavailable':
      return t('mcpmarket.error.unavailable', { detail: err.message });
    case 'mcpmarket_not_found':
      return t('mcpmarket.error.notFound');
    default:
      return err.message;
  }
}

/** A command line for reading, with words that hold spaces or quotes quoted. */
export function commandLine(words: string[]): string {
  return words.map((w) => (w === '' || /[\s"'`$]/.test(w) ? `"${w.replace(/(["\\$`])/g, '\\$1')}"` : w)).join(' ');
}

/** Where a variable goes, in words. */
export function envUse(t: T, use: string): string {
  if (use === 'bearer') return t('mcpmarket.install.useBearer');
  if (use.startsWith('header:')) return t('mcpmarket.install.useHeader', { name: use.slice('header:'.length) });
  return t('mcpmarket.install.useEnv');
}
