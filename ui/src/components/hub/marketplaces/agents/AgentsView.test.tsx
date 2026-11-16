// Fork-only (endgame302/skillshare): tests for the Agents marketplace view.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, expect, it, vi } from 'vitest';
import { api, ApiError } from '../../../../api/client';
import type { AgentListing, MarketAgent } from '../../../../api/forkAgents';
import { forkAgents } from '../../../../api/forkAgents';
import type { Target } from '../../../../api/types/targets';
import { I18nProvider } from '../../../../i18n';
import { ToastProvider } from '../../../Toast';
import AgentsView from './AgentsView';

vi.mock('../../../../api/forkAgents', async (original) => ({
  ...await original<typeof import('../../../../api/forkAgents')>(),
  forkAgents: { listing: vi.fn() },
}));

vi.mock('../../../../api/client', async (original) => {
  const mod = await original<typeof import('../../../../api/client')>();
  return {
    ...mod,
    api: {
      ...mod.api,
      listSkills: vi.fn(),
      installBatch: vi.fn(),
      listTargets: vi.fn(),
    },
  };
});

const agent = (name: string, extra: Partial<MarketAgent> = {}): MarketAgent => ({
  id: name,
  name,
  title: name,
  description: `${name} description`,
  category: '01-core-development',
  repo: 'VoltAgent/awesome-claude-code-subagents',
  dir: 'categories/01-core-development',
  file: `${name}.md`,
  ...extra,
});

const voltagentListing: AgentListing = {
  source: 'voltagent',
  repo: 'VoltAgent/awesome-claude-code-subagents',
  commit: 'abcdef1234567890',
  agents: [
    agent('api-designer', { title: 'API Designer', tools: ['Read', 'Write'], model: 'sonnet' }),
    agent('sql-pro', { title: 'SQL Pro', category: '02-data' }),
  ],
  fetchedAt: '2026-11-15T12:00:00Z',
  stale: false,
};

const aitmplListing: AgentListing = {
  source: 'aitmpl',
  repo: 'davila7/claude-code-templates',
  generatedAt: '2026-11-15T00:00:00Z',
  agents: [
    agent('security-reviewer', {
      title: 'Security Reviewer',
      category: 'security',
      repo: 'davila7/claude-code-templates',
      dir: 'cli-tool/components/agents/security',
      file: 'security-reviewer.md',
      downloads: 1234,
      scan: { tool: 'SkillSpector', severity: 'MEDIUM', score: 21 },
    }),
  ],
  fetchedAt: '2026-11-15T12:00:00Z',
  stale: false,
};

function renderView() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <I18nProvider>
          <ToastProvider>
            <AgentsView />
          </ToastProvider>
        </I18nProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

const row = (title: string) => screen.getByRole('button', { name: title }).closest('.ss-r') as HTMLElement;

beforeEach(() => {
  vi.clearAllMocks();
  // jsdom has no scrollIntoView, which the dropdown calls on its focused option.
  HTMLElement.prototype.scrollIntoView = vi.fn();
  vi.mocked(forkAgents.listing).mockResolvedValue(voltagentListing);
  vi.mocked(api.listSkills).mockResolvedValue({ resources: [] });
  vi.mocked(api.listTargets).mockResolvedValue({ targets: [], sourceSkillCount: 0 });
});

it('lists VoltAgent agents, filters by text, and installs via installBatch', async () => {
  vi.mocked(api.installBatch).mockResolvedValue({
    results: [{ name: 'api-designer', action: 'installed' }],
    summary: 'Installed 1 agent',
  });
  const user = userEvent.setup();
  renderView();

  await screen.findByRole('button', { name: 'API Designer' });
  expect(screen.getByRole('button', { name: 'SQL Pro' })).toBeInTheDocument();

  await user.type(screen.getByRole('textbox', { name: 'Filter agents' }), 'api');
  expect(screen.queryByRole('button', { name: 'SQL Pro' })).toBeNull();

  await user.click(within(row('API Designer')).getByRole('button', { name: 'Install' }));

  await waitFor(() => expect(api.installBatch).toHaveBeenCalledTimes(1));
  expect(api.installBatch).toHaveBeenCalledWith({
    source: 'github.com/VoltAgent/awesome-claude-code-subagents/categories/01-core-development',
    skills: [{ name: 'api-designer', path: 'api-designer.md' }],
    kind: 'agent',
  });
  expect(await screen.findByText('Installed agent api-designer')).toBeInTheDocument();
});

it('switches the Directory select to aitmpl and shows scan badge and installs count', async () => {
  vi.mocked(forkAgents.listing).mockImplementation(async (source) => (
    source === 'aitmpl' ? aitmplListing : voltagentListing
  ));
  const user = userEvent.setup();
  renderView();

  await screen.findByRole('button', { name: 'API Designer' });
  await user.click(screen.getByRole('combobox', { name: 'Directory' }));
  await user.click(screen.getByRole('option', { name: 'aitmpl.com' }));

  await waitFor(() => expect(forkAgents.listing).toHaveBeenCalledWith('aitmpl'));
  const secRow = await screen.findByRole('button', { name: 'Security Reviewer' }).then((el) => el.closest('.ss-r') as HTMLElement);
  expect(within(secRow).getByText('aitmpl scan: MEDIUM 21')).toBeInTheDocument();
  expect(within(secRow).getByText('1,234 installs')).toBeInTheDocument();
});

it('shows Installed and no Install button when the agent is already installed', async () => {
  vi.mocked(api.listSkills).mockResolvedValue({
    resources: [{ name: 'api-designer', kind: 'agent' }] as unknown as Awaited<ReturnType<typeof api.listSkills>>['resources'],
  });
  renderView();

  await screen.findByRole('button', { name: 'API Designer' });
  const apiRow = row('API Designer');
  expect(await within(apiRow).findByText('Installed')).toBeInTheDocument();
  expect(within(apiRow).queryByRole('button', { name: 'Install' })).toBeNull();
});

it('shows receiving targets and Codex setup note with link to targets page', async () => {
  vi.mocked(api.listTargets).mockResolvedValue({
    targets: [
      { name: 'claude', agentPath: '~/.claude/agents' },
      { name: 'codex' },
    ] as unknown as Target[],
    sourceSkillCount: 0,
  });
  renderView();

  await screen.findByText('Where installed agents go');
  expect(screen.getByText('claude')).toBeInTheDocument();
  expect(screen.getByText('~/.claude/agents')).toBeInTheDocument();
  const link = screen.getByRole('link', { name: 'Open the Codex target' });
  expect(link).toHaveAttribute('href', '/targets/codex?tab=agents');
});

it('shows skipped warning and row alert on install error result', async () => {
  vi.mocked(api.installBatch)
    .mockResolvedValueOnce({
      results: [{ name: 'api-designer', action: 'skipped' }],
      summary: 'Skipped 1',
    })
    .mockResolvedValueOnce({
      results: [{ name: 'sql-pro', error: 'security audit failed' }],
      summary: 'Failed 1',
    });
  const user = userEvent.setup();
  renderView();

  await screen.findByRole('button', { name: 'API Designer' });
  await user.click(within(row('API Designer')).getByRole('button', { name: 'Install' }));
  expect(await screen.findByText('api-designer is already in your agents folder; nothing was replaced.')).toBeInTheDocument();

  await user.click(within(row('SQL Pro')).getByRole('button', { name: 'Install' }));
  expect(await within(row('SQL Pro')).findByRole('alert')).toHaveTextContent('security audit failed');
});

it('shows rate-limit message on agents.rate_limited error and refetches on Try again', async () => {
  vi.mocked(forkAgents.listing).mockRejectedValueOnce(
    new ApiError(429, 'rate limit exceeded', { code: 'agents.rate_limited' }),
  );
  const user = userEvent.setup();
  renderView();

  const alert = await screen.findByRole('alert');
  expect(alert).toHaveTextContent('GitHub is rate limiting requests. Sign in with gh or set GITHUB_TOKEN, then try again.');

  await user.click(within(alert).getByRole('button', { name: 'Try again' }));
  expect(await screen.findByRole('button', { name: 'API Designer' })).toBeInTheDocument();
});
