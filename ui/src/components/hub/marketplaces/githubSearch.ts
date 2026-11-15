// Fork-only (endgame302/skillshare): the logic behind the GitHub marketplace on the Hubs page.
import type { SearchResult } from '../../../api/client';

/** What went wrong with a search, so the view can say what to do about it. */
export type SearchProblem = 'rateLimit' | 'auth' | 'other';

// The server passes on internal/github's and internal/search's error text.
export function searchProblem(message: string): SearchProblem {
  if (/rate limit/i.test(message)) return 'rateLimit';
  if (/requires authentication|bad credentials/i.test(message)) return 'auth';
  return 'other';
}

/**
 * One key for every way a source can name a GitHub repository, so a result matches the source
 * recorded for an installed skill (`github.com/o/r/p`, `https://github.com/o/r.git/p`, `o/r/p`).
 * Mirrors `sourceKey` in InstallDialog, which is not exported.
 */
export function sourceKey(source: string): string {
  const value = source.trim().replace(/\/+$/, '');
  const github = value.match(/^(?:(?:https?:\/\/)?(?:www\.)?github\.com\/|git@github\.com:)(.+)$/i)?.[1]
    ?? (/^[^/:.]+\/[^/]+/.test(value) ? value : null);
  return github ? `github:${github.replace(/\.git(?=\/|$)/i, '').toLowerCase()}` : value;
}

/** The sources of installed skills, as `sourceKey`s. */
export function installedSources(resources: { kind?: string; source?: string }[]): Set<string> {
  return new Set(resources.filter((r) => r.kind !== 'agent' && r.source).map((r) => sourceKey(r.source!)));
}

/** The page of the skill's folder on GitHub; `HEAD` is the default branch. */
export function githubURL(result: Pick<SearchResult, 'owner' | 'repo' | 'source'>): string | null {
  if (!result.owner || !result.repo) return null;
  const repo = `https://github.com/${result.owner}/${result.repo}`;
  const prefix = `${result.owner}/${result.repo}/`.toLowerCase();
  const path = result.source.toLowerCase().startsWith(prefix) ? result.source.slice(prefix.length).replace(/\/+$/, '') : '';
  return path ? `${repo}/tree/HEAD/${path.split('/').map(encodeURIComponent).join('/')}` : repo;
}
