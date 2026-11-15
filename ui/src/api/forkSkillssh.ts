// Fork-only (endgame302/skillshare): the skills.sh marketplace. Routes in
// internal/server/handler_fork_skillssh.go.
import { apiFetch } from './http';
import type { DiscoveredSkill } from './types/install';

export type SkillsshBoard = 'all' | 'trending' | 'hot';

export const SKILLSSH_BOARDS: SkillsshBoard[] = ['all', 'trending', 'hot'];

export const SKILLSSH_URL = 'https://skills.sh';

/** The page each list is read from, for "Open on skills.sh". */
export const SKILLSSH_BOARD_URL: Record<SkillsshBoard, string> = {
  all: SKILLSSH_URL + '/',
  trending: SKILLSSH_URL + '/trending',
  hot: SKILLSSH_URL + '/hot',
};

/** skills.sh's search refuses shorter queries. */
export const SKILLSSH_MIN_QUERY = 2;

export interface SkillsshSkill {
  /** `<source>/<skillId>` */
  id: string;
  /** A GitHub owner/repo, or a domain for skills published from a website. */
  source: string;
  skillId: string;
  name: string;
  installs: number;
  /** The skill's page on skills.sh. */
  url: string;
  /** Whether `source` is a GitHub repo skillshare can install from. */
  installable: boolean;
}

export interface SkillsshList {
  board?: SkillsshBoard;
  skills: SkillsshSkill[];
  fetchedAt: string;
  /** skills.sh could not be reached; this is an older copy. */
  stale?: boolean;
}

/** API error codes (`error_code`) the routes send. */
export const SKILLSSH_ERROR = {
  formatChanged: 'skillssh.format_changed',
  unreachable: 'skillssh.unreachable',
  notFound: 'skillssh.skill_not_found',
} as const;

export const forkSkillssh = {
  board: (name: SkillsshBoard) => apiFetch<SkillsshList>(`/fork/skillssh/board?name=${name}`),
  search: (q: string, limit = 50) =>
    apiFetch<SkillsshList>(`/fork/skillssh/search?q=${encodeURIComponent(q)}&limit=${limit}`),
  /** The folder in the repo that holds the skill skills.sh names; skills.sh ids are SKILL.md names. */
  resolve: (source: string, skillId: string) =>
    apiFetch<{ skill: DiscoveredSkill }>('/fork/skillssh/resolve', {
      method: 'POST',
      body: JSON.stringify({ source, skillId }),
    }),
};

// Query keys stay here rather than in lib/queryKeys.ts so the fork does not edit that file.
export const skillsshKeys = {
  board: (name: SkillsshBoard) => ['fork', 'skillssh', 'board', name] as const,
  search: (q: string) => ['fork', 'skillssh', 'search', q] as const,
};
