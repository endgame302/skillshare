// Fork-only (endgame302/skillshare): ClawHub list filtering.
import { describe, expect, it } from 'vitest';
import type { ClawHubItem } from '../../../../api/forkClawhub';
import { emptyFilter, filterClawHub, installedVersions, trustLevels } from './filter';

const item = (id: string, extra: Partial<ClawHubItem> = {}): ClawHubItem => {
  const [owner, slug] = id.slice(1).split('/');
  return {
    id, owner, slug, title: slug, description: '', version: '1.0.0', featured: false,
    publisher: owner, trust: 'official', source: 'clawhub', installable: true, ...extra,
  };
};

const awsCommit = 'b4416dddac9b6a0cc5412136e6dbf8f07ffdb31c';
const items = [
  item('@alipay/alipay-aipay', { title: '支付宝 AI 付接入', description: 'Alipay payments' }),
  item('@aws/agents-build', {
    source: 'github',
    version: awsCommit,
    description: 'Build AWS agents',
    github: {
      repo: 'aws/agent-toolkit-for-aws',
      path: 'plugins/aws-agents/skills/agents-build',
      commit: awsCommit,
      contentHash: 'e5a5',
    },
  }),
  item('@steipete/blogwatcher', { title: 'Blogwatcher', description: 'Monitor blogs and RSS/Atom feeds' }),
  item('@steipete/gog', { title: 'Gog', featured: true, description: 'Google Workspace CLI' }),
  item('@acme/thing', { trust: 'community' }),
];
const ids = (list: ClawHubItem[]) => list.map((it) => it.id);

describe('filterClawHub', () => {
  it('keeps everything with no filter, featured first', () => {
    expect(ids(filterClawHub(items, emptyFilter))).toEqual([
      '@steipete/gog', '@alipay/alipay-aipay', '@aws/agents-build', '@steipete/blogwatcher', '@acme/thing',
    ]);
  });

  it('matches every word against title, id, publisher and description, ignoring case', () => {
    expect(ids(filterClawHub(items, { ...emptyFilter, text: 'RSS blog' }))).toEqual(['@steipete/blogwatcher']);
    expect(ids(filterClawHub(items, { ...emptyFilter, text: 'steipete' }))).toEqual(['@steipete/gog', '@steipete/blogwatcher']);
    expect(ids(filterClawHub(items, { ...emptyFilter, text: '支付宝' }))).toEqual(['@alipay/alipay-aipay']);
    expect(filterClawHub(items, { ...emptyFilter, text: 'blog nothing' })).toEqual([]);
  });

  it('filters by source, featured and trust', () => {
    expect(ids(filterClawHub(items, { ...emptyFilter, source: 'github' }))).toEqual(['@aws/agents-build']);
    expect(filterClawHub(items, { ...emptyFilter, source: 'clawhub' })).toHaveLength(4);
    expect(ids(filterClawHub(items, { ...emptyFilter, featuredOnly: true }))).toEqual(['@steipete/gog']);
    expect(ids(filterClawHub(items, { ...emptyFilter, trust: 'community' }))).toEqual(['@acme/thing']);
  });
});

it('lists the trust levels in use', () => {
  expect(trustLevels(items)).toEqual(['community', 'official']);
});

it('finds installs by release folder or GitHub source and branch, not by name', () => {
  const skills = [
    { name: 'blogwatcher', source: String.raw`C:\Users\me\AppData\Roaming\skillshare\clawhub\releases\@steipete\blogwatcher@1.0.0` },
    { name: 'gog', source: 'github.com/someone/gog' },
    { name: 'thing', source: '/home/me/.local/share/skillshare/clawhub/releases/@acme/thing@2.1.0-beta.1' },
    { name: 'gone', source: '/home/me/.local/share/skillshare/clawhub/releases/@nobody/gone@1.0.0' },
    { name: 'mine', source: '/home/me/skills/mine' },
    { name: 'agents-build', source: 'github.com/aws/agent-toolkit-for-aws/plugins/aws-agents/skills/agents-build', branch: awsCommit },
    { name: 'other-aws', source: 'github.com/aws/agent-toolkit-for-aws/plugins/aws-agents/skills/other', branch: awsCommit },
    { name: 'no-branch', source: 'github.com/aws/agent-toolkit-for-aws/plugins/aws-agents/skills/agents-build' },
  ];
  expect([...installedVersions(skills, items)]).toEqual([
    ['@steipete/blogwatcher', '1.0.0'],
    ['@acme/thing', '2.1.0-beta.1'],
    ['@aws/agents-build', awsCommit],
  ]);
});

it('matches GitHub installs with protocol prefix and different case', () => {
  const skills = [
    { name: 'agents-build', source: 'https://GitHub.com/AWS/agent-toolkit-for-aws/plugins/aws-agents/skills/agents-build/', branch: 'deadbeef' },
  ];
  expect([...installedVersions(skills, items)]).toEqual([
    ['@aws/agents-build', 'deadbeef'],
  ]);
});
