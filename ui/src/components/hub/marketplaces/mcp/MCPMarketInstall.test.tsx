// Fork-only (endgame302/skillshare): adding an MCP marketplace entry to skillshare.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, expect, it, vi } from 'vitest';
import type { MCPMarketEntry, MCPMarketOption, MCPMarketPlan } from '../../../../api/forkMcpMarket';
import { forkMcpMarket } from '../../../../api/forkMcpMarket';
import { mcpApi } from '../../../../api/mcp';
import type { MCPMutation, MCPPlan } from '../../../../api/mcp';
import { I18nProvider } from '../../../../i18n';
import { ToastProvider } from '../../../Toast';
import MCPMarketInstall from './MCPMarketInstall';

vi.mock('../../../../api/forkMcpMarket', async (original) => ({
  ...await original<typeof import('../../../../api/forkMcpMarket')>(),
  forkMcpMarket: { list: vi.fn(), plan: vi.fn() },
}));
vi.mock('../../../../api/mcp', async (original) => ({
  ...await original<typeof import('../../../../api/mcp')>(),
  mcpApi: { list: vi.fn(), save: vi.fn(), preview: vi.fn() },
}));
// What the review shows is upstream's config view; here it only has to receive the mutation.
vi.mock('../../../mcp/MCPConfigView', () => ({
  default: ({ mutation }: { mutation: MCPMutation }) => <pre data-testid="config">{JSON.stringify(mutation)}</pre>,
}));
vi.mock('../../../mcp/MCPSyncBox', () => ({
  MCPSyncDialog: ({ shown }: { shown: number }) => <div role="dialog">Sync dialog with {shown} changes</div>,
}));

const entry: MCPMarketEntry = { name: 'io.github.acme/pkg', title: 'Pkg', description: 'Does things', version: '1.0.0', installable: true, repository: 'https://github.com/acme/pkg' };

const npm: MCPMarketOption = {
  id: 'package:0', kind: 'npm', label: 'npm pkg@1.0.0', runtime: 'npx', name: 'pkg',
  server: { command: 'npx', args: ['-y', 'pkg@1.0.0'], env: { API_KEY: { fromEnv: 'API_KEY' } } },
  command: ['npx', '-y', 'pkg@1.0.0'],
  envVars: [{ name: 'API_KEY', description: 'Your key', required: true, use: 'env' }],
  warnings: [{ code: 'first_run', params: { runtime: 'npx' } }],
};

const planWith = (options: MCPMarketOption[], extra: Partial<MCPMarketPlan> = {}): MCPMarketPlan => ({ source: 'official', entry, options, os: 'linux', dockerFound: true, ...extra });

function mcpList(targets: string[] | null, servers: Record<string, object> = {}) {
  vi.mocked(mcpApi.list).mockResolvedValue({ source: { path: '/x', configPath: '/x', targets, servers }, detected: [] } as unknown as Awaited<ReturnType<typeof mcpApi.list>>);
}

function renderDialog(onClose = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <I18nProvider>
          <ToastProvider>
            <MCPMarketInstall source="official" entry={entry} onClose={onClose} />
          </ToastProvider>
        </I18nProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
  return onClose;
}

beforeEach(() => {
  vi.clearAllMocks();
  HTMLElement.prototype.scrollIntoView = vi.fn();
  vi.mocked(forkMcpMarket.plan).mockResolvedValue(planWith([npm]));
  vi.mocked(mcpApi.save).mockResolvedValue({ applied: [], backupIds: [] });
  mcpList(['claude']);
});

it('shows what runs, reviews the config, adds the server and offers to sync', async () => {
  const user = userEvent.setup();
  renderDialog();
  expect(await screen.findByText('npx -y pkg@1.0.0')).toBeInTheDocument();
  expect(screen.getByText('API_KEY')).toBeInTheDocument();
  expect(screen.getByText('When an agent starts it, npx downloads and runs this code as you.')).toBeInTheDocument();
  expect(screen.getByRole('checkbox', { name: "Use skillshare's default MCP targets (Claude)" })).toBeChecked();

  await user.click(screen.getByRole('button', { name: 'Review config' }));
  const config = await screen.findByTestId('config');
  // The review shows the inherited targets; the saved server keeps inheriting them.
  expect(JSON.parse(config.textContent!)).toEqual({ name: 'pkg', server: { ...npm.server, targets: ['claude'] } });
  expect(forkMcpMarket.plan).toHaveBeenLastCalledWith('official', entry.name, {});

  await user.click(screen.getByRole('button', { name: 'Add to skillshare' }));
  await waitFor(() => expect(mcpApi.save).toHaveBeenCalledWith({ name: 'pkg', server: npm.server }));
  expect(await screen.findByText('Added pkg to skillshare.')).toBeInTheDocument();

  vi.mocked(mcpApi.preview).mockResolvedValue({ revision: 'r', sourcePath: '/x', blocked: false, changes: [{ target: 'claude', path: '/c', name: 'pkg', action: 'add' }] } as MCPPlan);
  await user.click(screen.getByRole('button', { name: 'Sync now…' }));
  expect(await screen.findByText('Sync dialog with 1 changes')).toBeInTheDocument();
});

it('sends the agents chosen instead of the defaults', async () => {
  const user = userEvent.setup();
  renderDialog();
  await user.click(await screen.findByRole('checkbox', { name: "Use skillshare's default MCP targets (Claude)" }));
  expect(screen.getByRole('button', { name: 'Review config' })).toBeDisabled();
  await user.click(screen.getByRole('checkbox', { name: 'Codex' }));
  await user.click(screen.getByRole('button', { name: 'Review config' }));
  await user.click(await screen.findByRole('button', { name: 'Add to skillshare' }));
  await waitFor(() => expect(mcpApi.save).toHaveBeenCalledWith({ name: 'pkg', server: { ...npm.server, targets: ['codex'] } }));
});

it('replaces a server of the same name only when asked', async () => {
  mcpList(['claude'], { pkg: { command: 'old' } });
  const user = userEvent.setup();
  renderDialog();
  expect(await screen.findByText('skillshare already has a server named pkg.')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Review config' })).toBeDisabled();
  await user.click(screen.getByRole('checkbox', { name: 'Replace it' }));
  await user.click(screen.getByRole('button', { name: 'Review config' }));
  await user.click(await screen.findByRole('button', { name: 'Add to skillshare' }));
  await waitFor(() => expect(mcpApi.save).toHaveBeenCalledWith({ name: 'pkg', server: npm.server, replace: true }));
});

it('explains a refused option and offers no install', async () => {
  vi.mocked(forkMcpMarket.plan).mockResolvedValue(planWith([{ id: 'remote:0', kind: 'sse', label: 'https://x.example/sse', name: 'pkg', refused: 'sse' }]));
  renderDialog();
  expect(await screen.findByText('Uses the SSE transport, which skillshare cannot write.')).toBeInTheDocument();
  expect(screen.getByRole('link', { name: 'Repository' })).toHaveAttribute('href', 'https://github.com/acme/pkg');
  expect(screen.queryByRole('button', { name: 'Review config' })).toBeNull();
});

it('asks for a required input and plans again with it', async () => {
  const waiting: MCPMarketOption = {
    ...npm, server: undefined, missing: ['package.0'],
    inputs: [{ key: 'package.0', label: 'workspace', description: 'Folder to serve', required: true }],
  };
  vi.mocked(forkMcpMarket.plan).mockImplementation(async (_s, _n, values) =>
    planWith([values?.['package.0'] ? { ...npm, inputs: [{ ...waiting.inputs![0], value: values['package.0'] }] } : waiting]));
  const user = userEvent.setup();
  renderDialog();
  expect(await screen.findByText('Required')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Review config' })).toBeDisabled();
  await user.type(screen.getByLabelText('workspace *'), '/srv');
  await waitFor(() => expect(forkMcpMarket.plan).toHaveBeenCalledWith('official', entry.name, { 'package.0': '/srv' }));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Review config' })).toBeEnabled());
});

it('notes untested npx on Windows for Codex and Gemini CLI', async () => {
  vi.mocked(forkMcpMarket.plan).mockResolvedValue(planWith([npm], { os: 'windows' }));
  mcpList(null);
  const user = userEvent.setup();
  renderDialog();
  expect(await screen.findByText('No default MCP targets are set. Choose where this server goes:')).toBeInTheDocument();
  expect(screen.queryByText(/were tested with Claude Code only/)).toBeNull();
  await user.click(screen.getByRole('checkbox', { name: 'Codex' }));
  expect(screen.getByText('On Windows, npx servers were tested with Claude Code only; Codex and Gemini CLI may fail to start them.')).toBeInTheDocument();
});
