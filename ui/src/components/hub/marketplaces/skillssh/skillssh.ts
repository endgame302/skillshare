// Fork-only (endgame302/skillshare): plain logic for the skills.sh marketplace view.
import type { Skill } from '../../../../api/client';
import type { SkillsshSkill } from '../../../../api/forkSkillssh';

/**
 * A name reduced to its letters and digits, for comparing a skill with a skills.sh id: skills.sh
 * lower-cases names and drops some punctuation (react:components is listed as reactcomponents).
 * The server's resolve route compares the same way.
 */
export function nameKey(name: string): string {
  return name.toLowerCase().replace(/[^\p{L}\p{N}]+/gu, '');
}

/**
 * The GitHub owner/repo (lower case) a recorded install source points into, or null. skillshare
 * records `owner/repo/path`, `github.com/owner/repo/path`, a URL, or an SSH address, and joins the
 * path with "/", "//" or ".git/".
 */
export function githubRepoOf(source: string): string | null {
  const value = source.trim();
  const rest =
    value.match(/^(?:(?:https?:\/\/)?(?:www\.)?github\.com\/|git@github\.com:)(.+)$/i)?.[1] ??
    // GitHub shorthand: an owner (letters, digits, hyphens; never a dot, unlike a host) and a repo.
    (/^[A-Za-z0-9][A-Za-z0-9-]*\/[^/]+/.test(value) ? value : null);
  if (!rest) return null;
  const [owner, repo] = rest.split('/');
  if (!owner || !repo) return null;
  return `${owner}/${repo.replace(/\.git$/i, '')}`.toLowerCase();
}

/**
 * The ids of the entries that are already installed: a skill recorded from the same GitHub repo
 * whose name, or the folder its source points at, is the entry's skill id. A skill with no GitHub
 * source (copied in, or installed by another tool) counts by name alone, as the hub view does.
 */
export function installedIds(resources: Skill[], entries: SkillsshSkill[]): Set<string> {
  const byRepo = new Map<string, Set<string>>();
  const byNameOnly = new Set<string>();
  for (const r of resources) {
    if (r.kind !== 'skill') continue;
    const repo = r.source ? githubRepoOf(r.source) : null;
    if (!repo) {
      byNameOnly.add(nameKey(r.name));
      continue;
    }
    const names = byRepo.get(repo) ?? new Set<string>();
    names.add(nameKey(r.name));
    const folder = (r.source ?? '').replace(/\/+$/, '').split('/').pop();
    if (folder) names.add(nameKey(folder));
    byRepo.set(repo, names);
  }
  const out = new Set<string>();
  for (const e of entries) {
    const id = nameKey(e.skillId);
    if (id && (byNameOnly.has(id) || byRepo.get(e.source.toLowerCase())?.has(id))) out.add(e.id);
  }
  return out;
}
