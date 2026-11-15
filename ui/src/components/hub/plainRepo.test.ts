// Fork-only: plain GitHub repos as hubs (endgame302/skillshare). Same cases as plainhub.Detect in Go.
import { describe, expect, it } from 'vitest';
import { isPlainRepoHub } from './plainRepo';

describe('isPlainRepoHub', () => {
  it.each([
    'anthropics/skills',
    '  anthropics/skills  ',
    'anthropics/skills/skills',
    'github.com/anthropics/skills',
    'https://github.com/anthropics/skills',
    'https://github.com/anthropics/skills/',
    'https://github.com/anthropics/skills.git',
    'https://github.com/anthropics/skills/tree/main/skills',
    'http://github.com/anthropics/skills',
  ])('takes %s for a repo', (url) => {
    expect(isPlainRepoHub(url)).toBe(true);
  });

  it.each([
    '',
    'skills',
    'https://raw.githubusercontent.com/runkids/skillshare-hub/main/skillshare-hub.json',
    'https://github.com/runkids/skillshare-hub/blob/main/skillshare-hub.json',
    'https://github.com/owner/repo/blob/main/README',
    'https://github.com/owner/repo/raw/main/hub',
    'https://github.com/owner/repo/blob/main/skills/pdf/SKILL.md',
    'owner/repo/hub.json',
    'https://example.com/hub.JSON?token=x',
    'https://example.com/hubs/team',
    'https://gitlab.com/owner/repo',
    'gitlab.com/owner/repo',
    'git@github.com:owner/repo.git',
    'ssh://git@github.com/owner/repo.git',
    'file:///tmp/repo',
    './hub',
    '/srv/hub',
    'C:\\hubs\\team',
  ])('leaves %s to the hub index reader', (url) => {
    expect(isPlainRepoHub(url)).toBe(false);
  });
});
