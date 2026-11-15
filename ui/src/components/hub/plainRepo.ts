/**
 * Whether a saved hub URL names a plain GitHub repo of skills rather than a skillshare-hub.json
 * (fork-only, endgame302/skillshare). The server builds such a hub's list from the repo's SKILL.md
 * files. Mirrors Detect in the Go package internal/plainhub, except that it cannot see whether a
 * path exists on disk.
 */
export function isPlainRepoHub(url: string): boolean {
  const s = url.trim().replace(/\/+$/, '');
  if (!s || /^(file|ssh):\/\//i.test(s) || /^[^\s@/]+@[^\s:/]+:/.test(s)) return false;
  if (/\.json$/i.test(s.split(/[?#]/)[0].replace(/\/+$/, ''))) return false;
  // owner/repo shorthand: no scheme, and a first segment that is not a domain or a local path.
  const shorthand = !/^[a-z][a-z0-9+.-]*:/i.test(s) && !/^[.~/\\]/.test(s) && !s.split('/')[0].includes('.');
  const path = shorthand ? s : /^(?:https?:\/\/)?github\.com\/(.+)$/i.exec(s)?.[1];
  if (!path) return false;
  const [owner, repo, kind] = path.split('/');
  return Boolean(owner && repo) && /^[\w.-]+$/.test(owner) && /^[\w.-]+$/.test(repo) && kind !== 'blob' && kind !== 'raw';
}
