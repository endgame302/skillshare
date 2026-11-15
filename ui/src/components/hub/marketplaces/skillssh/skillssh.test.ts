// Fork-only (endgame302/skillshare).
import { describe, expect, it } from 'vitest';
import type { Skill } from '../../../../api/client';
import type { SkillsshSkill } from '../../../../api/forkSkillssh';
import { githubRepoOf, installedIds, nameKey } from './skillssh';

const entry = (source: string, skillId: string): SkillsshSkill => ({
  id: `${source}/${skillId}`,
  source,
  skillId,
  name: skillId,
  installs: 1,
  url: `https://skills.sh/${source}/${skillId}`,
  installable: true,
});

const resource = (name: string, source?: string, kind: 'skill' | 'agent' = 'skill') =>
  ({ name, source, kind, flatName: name, relPath: name, sourcePath: '', isInRepo: false }) as Skill;

describe('nameKey', () => {
  it('keeps only letters and digits, so a name and its skills.sh id compare equal', () => {
    expect(nameKey('react:components')).toBe(nameKey('reactcomponents'));
    expect(nameKey('stitch::react-components')).toBe(nameKey('stitchreact-components'));
    expect(nameKey('Web Design Guidelines')).toBe(nameKey('web-design-guidelines'));
    expect(nameKey('日本語 スキル')).toBe('日本語スキル');
  });
});

describe('githubRepoOf', () => {
  it.each([
    ['vercel-labs/agent-skills/skills/react-best-practices', 'vercel-labs/agent-skills'],
    ['github.com/Vercel-Labs/agent-skills/skills/x', 'vercel-labs/agent-skills'],
    ['https://github.com/a/b.git//skills/x', 'a/b'],
    ['git@github.com:a/b.git', 'a/b'],
    ['a/b', 'a/b'],
  ])('%s → %s', (source, repo) => expect(githubRepoOf(source)).toBe(repo));

  it.each(['gitlab.com/a/b/c', '/home/me/skills/x', 'C:\\skills\\x', 'open.feishu.cn'])('%s is not a GitHub repo', (source) => {
    expect(githubRepoOf(source)).toBeNull();
  });
});

describe('installedIds', () => {
  it('matches a skill from the same repo by its name or the folder its source names', () => {
    const entries = [
      entry('vercel-labs/agent-skills', 'vercel-react-best-practices'),
      entry('vercel-labs/agent-skills', 'vercel-composition-patterns'),
      entry('vercel-labs/skills', 'find-skills'),
      entry('other/repo', 'find-skills'),
    ];
    const resources = [
      resource('vercel-react-best-practices', 'github.com/vercel-labs/agent-skills/skills/react-best-practices'),
      resource('whatever', 'vercel-labs/skills/skills/find-skills'),
      resource('vercel-composition-patterns', 'someone-else/agent-skills/skills/composition-patterns'),
      resource('vercel-composition-patterns', 'vercel-labs/agent-skills/agents/x.md', 'agent'),
    ];
    expect([...installedIds(resources, entries)].sort()).toEqual([
      'vercel-labs/agent-skills/vercel-react-best-practices',
      'vercel-labs/skills/find-skills',
    ]);
  });

  it('ignores the punctuation skills.sh drops from ids', () => {
    // skills.sh lists the SKILL.md name react:components under the id reactcomponents.
    const entries = [entry('google-labs-code/stitch-skills', 'reactcomponents'), entry('google-labs-code/stitch-skills', 'stitchreact-native')];
    const resources = [
      resource('react-components', 'github.com/google-labs-code/stitch-skills/plugins/stitch-build/skills/react-components'),
      resource('stitch::react-native', undefined),
    ];
    expect([...installedIds(resources, entries)].sort()).toEqual([
      'google-labs-code/stitch-skills/reactcomponents',
      'google-labs-code/stitch-skills/stitchreact-native',
    ]);
  });

  it('matches a skill with no GitHub source by name alone', () => {
    const entries = [entry('vercel-labs/skills', 'find-skills'), entry('other/repo', 'find-skills'), entry('a/b', 'tdd')];
    const resources = [resource('find-skills', undefined), resource('tdd', 'D:\\skills\\tdd', 'agent')];
    expect([...installedIds(resources, entries)].sort()).toEqual(['other/repo/find-skills', 'vercel-labs/skills/find-skills']);
  });
});
