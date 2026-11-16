// Fork-only (endgame302/skillshare): the ClawHub marketplace view.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, expect, it, vi } from 'vitest';
import { api, ApiError } from '../../../../api/client';
import type { ClawHubFeed, ClawHubItem } from '../../../../api/forkClawhub';
import { forkClawhub } from '../../../../api/forkClawhub';
import { I18nProvider } from '../../../../i18n';
import { ToastProvider } from '../../../Toast';
import ClawHubView from './ClawHubView';

vi.mock('../../../../api/forkClawhub', async (original) => ({
  ...await original<typeof import('../../../../api/forkClawhub')>(),
  forkClawhub: { feed: vi.fn(), install: vi.fn() },
}));
vi.mock('../../../../api/client', async (original) => {
  const mod = await original<typeof import('../../../../api/client')>();
  return { ...mod, api: { ...mod.api, listSkills: vi.fn() } };
});

const item = (id: string, extra: Partial<ClawHubItem> = {}): ClawHubItem => {
  const [owner, slug] = id.slice(1).split('/');
  return {
    id, owner, slug, title: slug, description: `${slug} description`, version: '1.0.0', featured: false,
    publisher: owner, trust: 'official', source: 'clawhub', installable: true, integrity: 'sha256:' + 'a'.repeat(64), ...extra,
  };
};

const awsCommit = 'b4416dddac9b6a0cc5412136e6dbf8f07ffdb31c';

const feed: ClawHubFeed = {
  items: [
    item('@steipete/blogwatcher', { title: 'Blogwatcher' }),
    item('@steipete/gog', { title: 'Gog', featured: true }),
    item('@aws/agents-build', {
      title: 'agents-build', source: 'github', version: awsCommit, installable: true, integrity: 'sha256:' + 'b'.repeat(64),
      github: { repo: 'aws/agent-toolkit-for-aws', path: 'plugins/aws-agents/skills/agents-build', commit: awsCommit, contentHash: 'x' },
    }),
  ],
  generatedAt: '2026-11-15T00:42:19.926Z',
  expiresAt: '2026-11-22T00:42:18.813Z',
  fetchedAt: '2026-11-15T12:00:00Z',
  registry: 'https://clawhub.ai',
  stale: false,
};

function renderView() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <I18nProvider>
        <ToastProvider>
          <ClawHubView />
        </ToastProvider>
      </I18nProvider>
    </QueryClientProvider>,
  );
}

const row = (title: string) => screen.getByRole('button', { name: title }).closest('.ss-r') as HTMLElement;

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(forkClawhub.feed).mockResolvedValue(feed);
  vi.mocked(api.listSkills).mockResolvedValue({ resources: [] });
});

it('installs the version the list shows, without forcing', async () => {
  vi.mocked(forkClawhub.install).mockResolvedValue({ skillName: 'blogwatcher', action: 'copied', origin: 'clawhub://@steipete/blogwatcher@1.0.0', version: '1.0.0' });
  const user = userEvent.setup();
  renderView();

  await screen.findByRole('button', { name: 'Blogwatcher' });
  const awsRow = row('agents-build');
  expect(within(awsRow).getByText('On GitHub')).toBeInTheDocument();
  expect(within(awsRow).getByRole('button', { name: 'Install' })).toBeInTheDocument();

  await user.type(screen.getByRole('textbox', { name: 'Filter skills' }), 'blog');
  expect(screen.queryByRole('button', { name: 'Gog' })).toBeNull();
  await user.click(within(row('Blogwatcher')).getByRole('button', { name: 'Install' }));

  await waitFor(() => expect(forkClawhub.install).toHaveBeenCalledTimes(1));
  expect(forkClawhub.install).toHaveBeenCalledWith('@steipete/blogwatcher', '1.0.0', false);
});

it('installs a GitHub-hosted entry at its pinned commit and shows a commit toast', async () => {
  vi.mocked(forkClawhub.install).mockResolvedValue({
    skillName: 'agents-build',
    action: 'cloned',
    origin: `clawhub://@aws/agents-build@${awsCommit}`,
    version: awsCommit,
    github: 'github.com/aws/agent-toolkit-for-aws/plugins/aws-agents/skills/agents-build',
  });
  const user = userEvent.setup();
  renderView();

  await screen.findByRole('button', { name: 'agents-build' });
  await user.click(within(row('agents-build')).getByRole('button', { name: 'Install' }));

  await waitFor(() => expect(forkClawhub.install).toHaveBeenCalledWith('@aws/agents-build', awsCommit, false));
  expect(await screen.findByText('Installed agents-build from GitHub at commit b4416dd')).toBeInTheDocument();
});

it('recognises a GitHub-hosted entry as installed by its source and commit branch', async () => {
  vi.mocked(api.listSkills).mockResolvedValue({
    resources: [{
      name: 'agents-build',
      source: 'github.com/aws/agent-toolkit-for-aws/plugins/aws-agents/skills/agents-build',
      branch: awsCommit,
    }] as unknown as Awaited<ReturnType<typeof api.listSkills>>['resources'],
  });
  renderView();

  await screen.findByRole('button', { name: 'agents-build' });
  const awsRow = row('agents-build');
  expect(await within(awsRow).findByText('Installed')).toBeInTheDocument();
  expect(within(awsRow).queryByRole('button', { name: 'Install' })).toBeNull();
});

it('asks before replacing a skill of the same name, and replaces only on confirm', async () => {
  // As the server does: a taken name is refused unless the request says to replace it.
  vi.mocked(forkClawhub.install).mockImplementation(async (_id, _version, force) => {
    if (!force) throw new ApiError(409, 'a skill named blogwatcher is already installed', { code: 'clawhub_exists', params: { name: 'blogwatcher' } });
    return { skillName: 'blogwatcher', action: 'copied', origin: 'clawhub://@steipete/blogwatcher@1.0.0', version: '1.0.0' };
  });
  const user = userEvent.setup();
  renderView();

  await screen.findByRole('button', { name: 'Blogwatcher' });
  await user.click(within(row('Blogwatcher')).getByRole('button', { name: 'Install' }));
  const dialog = await screen.findByRole('dialog', { name: 'Replace blogwatcher?' });
  expect(forkClawhub.install).toHaveBeenCalledTimes(1);

  await user.click(within(dialog).getByRole('button', { name: 'Cancel' }));
  expect(forkClawhub.install).toHaveBeenCalledTimes(1);

  await user.click(within(row('Blogwatcher')).getByRole('button', { name: 'Install' }));
  await user.click(within(await screen.findByRole('dialog', { name: 'Replace blogwatcher?' })).getByRole('button', { name: 'Replace' }));
  await waitFor(() => expect(forkClawhub.install).toHaveBeenCalledTimes(3));
  expect(forkClawhub.install).toHaveBeenLastCalledWith('@steipete/blogwatcher', '1.0.0', true);
});

it('says a download failed its checksum, and does not show it as installed', async () => {
  vi.mocked(forkClawhub.install).mockRejectedValue(new ApiError(502, 'the downloaded archive does not match', { code: 'clawhub_integrity_mismatch' }));
  const user = userEvent.setup();
  renderView();

  await screen.findByRole('button', { name: 'Blogwatcher' });
  await user.click(within(row('Blogwatcher')).getByRole('button', { name: 'Install' }));
  expect(await within(row('Blogwatcher')).findByRole('alert')).toHaveTextContent("does not match the checksum in ClawHub's feed");
  expect(within(row('Blogwatcher')).getByRole('button', { name: 'Install' })).toBeInTheDocument();
});

it('shows why the feed could not load, and retries', async () => {
  vi.mocked(forkClawhub.feed).mockRejectedValueOnce(new ApiError(502, 'cannot reach ClawHub: timeout', { code: 'clawhub_feed_unavailable' }));
  const user = userEvent.setup();
  renderView();

  const alert = await screen.findByRole('alert');
  expect(alert).toHaveTextContent("Could not load ClawHub's skills: cannot reach ClawHub: timeout");
  await user.click(within(alert).getByRole('button', { name: 'Try again' }));
  expect(await screen.findByRole('button', { name: 'Blogwatcher' })).toBeInTheDocument();
});
