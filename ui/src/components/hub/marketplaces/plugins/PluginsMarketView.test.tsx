// Fork-only (endgame302/skillshare): tests for the Plugins marketplace view and its two install dialogs.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, expect, it, vi } from 'vitest';
import { ApiError } from '../../../../api/client';
import type { CatalogEntry, HandoffPlan, PluginCatalog } from '../../../../api/forkPlugins';
import { forkPlugins } from '../../../../api/forkPlugins';
import type { PluginDiscovery } from '../../../../api/plugins';
import { pluginsApi } from '../../../../api/plugins';
import { I18nProvider } from '../../../../i18n';
import { ToastProvider } from '../../../Toast';
import { commandLine, isInstalled, matchesText } from './helpers';
import PluginsMarketView from './PluginsMarketView';

vi.mock('../../../../api/forkPlugins', async (original) => ({
  ...await original<typeof import('../../../../api/forkPlugins')>(),
  forkPlugins: { catalog: vi.fn(), handoff: vi.fn() },
}));
vi.mock('../../../../api/plugins', async (original) => {
  const mod = await original<typeof import('../../../../api/plugins')>();
  return { ...mod, pluginsApi: { ...mod.pluginsApi, list: vi.fn(), discover: vi.fn(), preview: vi.fn(), apply: vi.fn() } };
});

const commit = 'c'.repeat(40);
const sha = 'a'.repeat(40);
const market = 'claude-plugins-official';

const entry = (name: string, extra: Partial<CatalogEntry>): CatalogEntry => ({
  name, description: `${name} description`, source: { kind: 'path', path: `plugins/${name}` }, install: 'add', pinned: true, ...extra,
});

const catalog: PluginCatalog = {
  id: 'official',
  repo: 'anthropics/claude-plugins-official',
  market,
  commit,
  entries: [
    entry('agent-sdk-dev', { category: 'development', author: 'Anthropic', homepage: 'https://example.com/sdk' }),
    entry('agentforce-adlc', { source: { kind: 'url', url: 'https://github.com/x/adlc.git', sha }, install: 'native', pinned: true }),
    entry('xcode', { source: { kind: 'command', command: 'xcode-plugin' }, install: 'manual', pinned: false }),
    entry('broken', { source: { kind: 'path' }, install: 'none', pinned: false, problem: 'path escapes the catalog' }),
  ],
  fetchedAt: '2026-11-17T00:00:00Z',
  stale: false,
};

const discovery: PluginDiscovery = {
  source: 'https://github.com/anthropics/claude-plugins-official',
  sourceRef: '',
  digest: 'd',
  targetDefinitions: [{ target: 'claude', label: 'Claude Code', project: true, operations: ['add'] }],
  candidates: [{ name: 'agent-sdk-dev', description: '', version: '', targets: ['claude'], components: ['skills'] }],
};

const handoffPlan: HandoffPlan = {
  claude: 'C:\\bin\\claude.exe',
  claudeVersion: '2.1.0 (Claude Code)',
  market,
  pluginId: `agentforce-adlc@${market}`,
  registered: false,
  steps: [
    { name: 'add', args: ['plugin', 'marketplace', 'add', 'anthropics/claude-plugins-official', '--scope', 'user', '--json'] },
    { name: 'install', args: ['plugin', 'install', `agentforce-adlc@${market}`, '--scope', 'user', '--json'] },
  ],
  import: `skillshare plugin import agentforce-adlc@${market} --from claude`,
};

function renderView() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <I18nProvider>
          <ToastProvider>
            <PluginsMarketView />
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
  vi.mocked(forkPlugins.catalog).mockResolvedValue(catalog);
  vi.mocked(pluginsApi.list).mockResolvedValue({ packages: {}, hosts: [] });
});

it('lists every entry with how it installs, and filters by text', async () => {
  const user = userEvent.setup();
  renderView();

  await screen.findByRole('button', { name: 'agent-sdk-dev' });
  expect(forkPlugins.catalog).toHaveBeenCalledWith('official');
  expect(within(row('agent-sdk-dev')).getByRole('button', { name: 'Install' })).toBeInTheDocument();
  expect(within(row('agent-sdk-dev')).getByText('In catalog')).toBeInTheDocument();
  expect(within(row('agent-sdk-dev')).getByRole('link', { name: 'Open the homepage' })).toHaveAttribute('href', 'https://example.com/sdk');
  expect(within(row('agentforce-adlc')).getByText('url')).toBeInTheDocument();
  expect(within(row('agentforce-adlc')).getByText('Pinned')).toBeInTheDocument();
  expect(within(row('xcode')).getByText('Install in Claude Code')).toHaveAttribute('title', `/plugin install xcode@${market}`);
  expect(within(row('xcode')).queryByRole('button', { name: 'Install' })).toBeNull();
  expect(within(row('broken')).getByText('Not installable here')).toHaveAttribute('title', 'path escapes the catalog');

  await user.type(screen.getByRole('textbox', { name: 'Filter plugins' }), 'sdk');
  expect(screen.getByRole('button', { name: 'agent-sdk-dev' })).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'xcode' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'agentforce-adlc' })).toBeNull();
});

it('installs a catalog-stored plugin through skillshare, pinned to the catalog commit', async () => {
  vi.mocked(pluginsApi.discover).mockResolvedValue(discovery);
  vi.mocked(pluginsApi.preview).mockResolvedValue({
    revision: 'r1', blocked: false,
    changes: [{ name: 'agent-sdk-dev', target: 'claude', id: 'agent-sdk-dev@x', action: 'install' }],
  });
  vi.mocked(pluginsApi.apply).mockResolvedValue({ result: { results: [] }, failure: '' });
  const user = userEvent.setup();
  renderView();

  await user.click(within(await screen.findByRole('button', { name: 'agent-sdk-dev' }).then((b) => b.closest('.ss-r') as HTMLElement)).getByRole('button', { name: 'Install' }));
  await waitFor(() => expect(pluginsApi.discover).toHaveBeenCalledWith('anthropics/claude-plugins-official', commit));

  await user.click(await screen.findByRole('checkbox', { name: 'Claude Code' }));
  await user.click(screen.getByRole('button', { name: 'Review changes' }));
  const request = { action: 'add', source: discovery.source, sourceRef: commit, plugin: 'agent-sdk-dev', targets: ['claude'] };
  await waitFor(() => expect(pluginsApi.preview).toHaveBeenCalledWith(request));

  await user.click(await screen.findByRole('button', { name: 'Apply changes' }));
  await waitFor(() => expect(pluginsApi.apply).toHaveBeenCalledWith(request, 'r1'));
  expect(await screen.findByText('Installed agent-sdk-dev')).toBeInTheDocument();
});

it('shows the catalog has no such plugin when discovery does not list it', async () => {
  vi.mocked(pluginsApi.discover).mockResolvedValue({ ...discovery, candidates: [] });
  const user = userEvent.setup();
  renderView();

  await user.click(within(await screen.findByRole('button', { name: 'agent-sdk-dev' }).then((b) => b.closest('.ss-r') as HTMLElement)).getByRole('button', { name: 'Install' }));
  expect(await screen.findByText('The catalog at this commit has no plugin named agent-sdk-dev.')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Review changes' })).toBeDisabled();
});

it('shows the exact Claude Code commands and runs them only after the click, then imports', async () => {
  vi.mocked(forkPlugins.handoff).mockImplementation(async (body) => (
    body.dryRun ? { plan: handoffPlan } : { plan: handoffPlan, result: { pluginId: handoffPlan.pluginId, message: 'Installed' } }
  ));
  vi.mocked(pluginsApi.preview).mockResolvedValue({
    revision: 'r2', blocked: false,
    changes: [{ name: 'agentforce-adlc', target: 'claude', id: handoffPlan.pluginId, action: 'import' }],
  });
  vi.mocked(pluginsApi.apply).mockResolvedValue({ result: { results: [] }, failure: '' });
  vi.mocked(pluginsApi.list).mockResolvedValue({ packages: {}, hosts: [], targetDefinitions: discovery.targetDefinitions });
  const user = userEvent.setup();
  renderView();

  await screen.findByRole('button', { name: 'agentforce-adlc' });
  await user.click(within(row('agentforce-adlc')).getByRole('button', { name: 'Install' }));
  await waitFor(() => expect(forkPlugins.handoff).toHaveBeenCalledWith({ catalog: 'official', name: 'agentforce-adlc', commit, sha, dryRun: true }));
  expect(await screen.findByText(/claude plugin marketplace add anthropics\/claude-plugins-official --scope user --json/)).toBeInTheDocument();
  expect(screen.getByText(/claude plugin install agentforce-adlc@claude-plugins-official --scope user --json/)).toBeInTheDocument();
  expect(screen.getByText(handoffPlan.import)).toBeInTheDocument();
  expect(forkPlugins.handoff).toHaveBeenCalledTimes(1);

  await user.click(screen.getByRole('button', { name: 'Run in Claude Code' }));
  await waitFor(() => expect(forkPlugins.handoff).toHaveBeenLastCalledWith({ catalog: 'official', name: 'agentforce-adlc', commit, sha, dryRun: false }));
  await waitFor(() => expect(pluginsApi.preview).toHaveBeenCalledWith({ action: 'import', from: 'claude', plugin: handoffPlan.pluginId }));

  const applyButton = await screen.findByRole('button', { name: 'Apply changes' });
  // The import row names the Agent as the Plugins page does.
  expect(within(screen.getByRole('dialog')).getByText('Claude Code')).toBeInTheDocument();
  await user.click(applyButton);
  await waitFor(() => expect(pluginsApi.apply).toHaveBeenCalledWith({ action: 'import', from: 'claude', plugin: handoffPlan.pluginId }, 'r2'));
  expect(await screen.findByText(`Installed ${handoffPlan.pluginId} with Claude Code and added it to skillshare`)).toBeInTheDocument();
});

it('tells how to install Claude Code when claude is not on PATH, with no Run button', async () => {
  vi.mocked(forkPlugins.handoff).mockResolvedValue({ plan: { ...handoffPlan, claude: '', claudeVersion: undefined, steps: [] } });
  const user = userEvent.setup();
  renderView();

  await screen.findByRole('button', { name: 'agentforce-adlc' });
  await user.click(within(row('agentforce-adlc')).getByRole('button', { name: 'Install' }));
  expect(await screen.findByText('irm https://claude.ai/install.ps1 | iex')).toBeInTheDocument();
  expect(screen.getByRole('link', { name: 'Claude Code setup guide' })).toHaveAttribute('href', 'https://code.claude.com/docs/en/setup');
  expect(screen.queryByRole('button', { name: 'Run in Claude Code' })).toBeNull();
});

it('asks for a refresh when the catalog moved under the hand-off', async () => {
  vi.mocked(forkPlugins.handoff).mockImplementation(async (body) => {
    if (body.dryRun) return { plan: handoffPlan };
    throw new ApiError(409, 'moved', { code: 'plugins_market.catalog_moved' });
  });
  const user = userEvent.setup();
  renderView();

  await screen.findByRole('button', { name: 'agentforce-adlc' });
  await user.click(within(row('agentforce-adlc')).getByRole('button', { name: 'Install' }));
  await user.click(await screen.findByRole('button', { name: 'Run in Claude Code' }));
  expect(await screen.findByText('The catalog changed since this list was loaded. Refresh the catalog and try again.')).toBeInTheDocument();
  expect(pluginsApi.preview).not.toHaveBeenCalled();
});

it('marks a plugin skillshare already manages as installed', async () => {
  vi.mocked(pluginsApi.list).mockResolvedValue({ packages: { 'agent-sdk-dev': { bindings: {} } }, hosts: [] });
  renderView();

  await screen.findByRole('button', { name: 'agent-sdk-dev' });
  expect(await within(row('agent-sdk-dev')).findByText('Installed')).toBeInTheDocument();
  expect(within(row('agent-sdk-dev')).queryByRole('button', { name: 'Install' })).toBeNull();
});

it('shows the rate-limit text and retries', async () => {
  vi.mocked(forkPlugins.catalog).mockRejectedValueOnce(new ApiError(502, 'rate limited', { code: 'plugins_market.rate_limited' }));
  const user = userEvent.setup();
  renderView();

  const alert = await screen.findByRole('alert');
  expect(alert).toHaveTextContent('GitHub is rate limiting requests. Sign in with gh or set GITHUB_TOKEN, then try again.');
  await user.click(within(alert).getByRole('button', { name: 'Try again' }));
  expect(await screen.findByRole('button', { name: 'agent-sdk-dev' })).toBeInTheDocument();
});

it('matches installed plugins by name or by a binding to name@market', () => {
  const e = catalog.entries[1];
  expect(isInstalled(undefined, e, market)).toBe(false);
  expect(isInstalled({ packages: {}, hosts: [] }, e, market)).toBe(false);
  expect(isInstalled({ packages: { adlc: { bindings: { claude: { id: `agentforce-adlc@${market}` } } } }, hosts: [] }, e, market)).toBe(true);
  expect(isInstalled({ packages: { adlc: { bindings: { claude: { id: 'agentforce-adlc@other' } } } }, hosts: [] }, e, market)).toBe(false);
});

it('filters on keywords and author too, and quotes command arguments with spaces', () => {
  expect(matchesText({ ...catalog.entries[0], keywords: ['typescript'] }, 'typescript')).toBe(true);
  expect(matchesText(catalog.entries[0], 'anthropic')).toBe(true);
  expect(matchesText(catalog.entries[0], 'nothing-like-it')).toBe(false);
  expect(commandLine(['plugin', 'install', 'a@b', '--scope', 'user'])).toBe('claude plugin install a@b --scope user');
  expect(commandLine(['plugin', 'marketplace', 'add', 'C:\\My Plugins'])).toBe('claude plugin marketplace add "C:\\My Plugins"');
});
