// Fork-only: the GitHub marketplace's helpers (endgame302/skillshare).
import { describe, expect, it } from 'vitest';
import { githubURL, installedSources, searchProblem, sourceKey } from './githubSearch';

describe('searchProblem', () => {
  it('tells a rate limit from a missing token and other failures, by the text the server sends', () => {
    expect(searchProblem("GitHub API rate limit exceeded (0/60 remaining); set GITHUB_TOKEN/GH_TOKEN or run 'gh auth login'")).toBe('rateLimit');
    expect(searchProblem('GitHub Code Search API requires authentication')).toBe('auth');
    expect(searchProblem('github search failed: 422 Validation Failed')).toBe('other');
  });
});

describe('installed sources', () => {
  it('matches a result to the source an installed skill records, however it is written', () => {
    const installed = installedSources([
      { kind: 'skill', source: 'https://github.com/Anthropics/Skills.git/skills/pdf' },
      { kind: 'agent', source: 'github.com/acme/agents/reviewer' },
      { kind: 'skill' },
    ]);
    expect(installed.has(sourceKey('anthropics/skills/skills/pdf'))).toBe(true);
    expect(installed.has(sourceKey('anthropics/skills/skills/docx'))).toBe(false);
    // An agent at that source is not the skill a result offers.
    expect(installed.has(sourceKey('acme/agents/reviewer'))).toBe(false);
  });
});

describe('githubURL', () => {
  it('links the skill folder on the default branch, or the repository for a root skill', () => {
    expect(githubURL({ owner: 'anthropics', repo: 'skills', source: 'anthropics/skills/skills/pdf' }))
      .toBe('https://github.com/anthropics/skills/tree/HEAD/skills/pdf');
    expect(githubURL({ owner: 'acme', repo: 'one-skill', source: 'acme/one-skill' })).toBe('https://github.com/acme/one-skill');
    expect(githubURL({ owner: 'acme', repo: 'r', source: 'acme/r/my skill#1' })).toBe('https://github.com/acme/r/tree/HEAD/my%20skill%231');
    expect(githubURL({ owner: '', repo: '', source: 'x' })).toBeNull();
  });
});
