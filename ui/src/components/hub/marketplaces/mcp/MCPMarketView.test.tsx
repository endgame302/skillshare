// Fork-only (endgame302/skillshare): the MCP marketplace list.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, expect, it, vi } from 'vitest';
import type { MCPMarketEntry, MCPMarketList, MCPMarketSource } from '../../../../api/forkMcpMarket';
import { forkMcpMarket } from '../../../../api/forkMcpMarket';
import { I18nProvider } from '../../../../i18n';
import { ToastProvider } from '../../../Toast';
import MCPMarketView from './MCPMarketView';

vi.mock('../../../../api/forkMcpMarket', async (original) => ({
  ...await original<typeof import('../../../../api/forkMcpMarket')>(),
  forkMcpMarket: { list: vi.fn(), plan: vi.fn() },
}));

const entry = (name: string, extra: Partial<MCPMarketEntry> = {}): MCPMarketEntry => ({
  name, title: name.split('/').pop(), description: `${name} description`, version: '1.0.0', status: 'active', installable: true, ...extra,
});

const listOf = (source: MCPMarketSource, items: MCPMarketEntry[], extra: Partial<MCPMarketList> = {}): MCPMarketList => ({
  source, items, total: items.length, hiddenBulk: 0, hiddenDeprecated: 0, bulkThreshold: 50,
  status: { state: 'ready', entries: items.length, complete: true, syncing: false, stale: false, fetchedAt: '2026-11-17T00:00:00Z' },
  ...extra,
});

function renderView() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <I18nProvider>
          <ToastProvider>
            <MCPMarketView />
          </ToastProvider>
        </I18nProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

const row = (title: string) => screen.getByRole('button', { name: title }).closest('.ss-r') as HTMLElement;

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(forkMcpMarket.list).mockImplementation(async (source) => {
    if (source === 'docker') {
      return listOf('docker', [entry('brave', { title: 'Brave Search', kinds: ['docker'], local: true, dockerBuilt: true, pulls: 12000 })], { dockerFound: false });
    }
    return listOf(source, [
      entry('io.github.brave/brave-search-mcp-server', { title: 'Brave Search', kinds: ['npm'], local: true }),
      entry('io.github.evozim/jail', { title: 'Jail', kinds: ['sse'], remote: true, installable: false, blocked: 'sse' }),
    ], { hiddenBulk: 120, hiddenDeprecated: 3, status: { state: 'loading', entries: 2400, complete: false, syncing: true, stale: false } });
  });
});

it('lists the official registry with kinds, refusals and the indexing note', async () => {
  renderView();
  await screen.findByRole('button', { name: 'Brave Search' }, { timeout: 5000 });
  expect(forkMcpMarket.list).toHaveBeenCalledWith('official', expect.objectContaining({ sort: 'name', offset: 0, limit: 50, bulk: false }));
  expect(within(row('Brave Search')).getByText('npm')).toBeInTheDocument();
  expect(within(row('Brave Search')).getByRole('button', { name: 'Add…' })).toBeInTheDocument();
  const blocked = within(row('Jail')).getByText('Not supported');
  expect(blocked).toHaveAttribute('title', 'Uses the SSE transport, which skillshare cannot write.');
  expect(screen.getByText(/Building a local index of the official registry: 2,400 servers so far/)).toBeInTheDocument();
  expect(screen.getByText(/3 deprecated servers are hidden/)).toBeInTheDocument();
  expect(screen.getByText(/A registry listing proves who controls a namespace/)).toBeInTheDocument();
});

it('searches after a pause, by relevance', async () => {
  const user = userEvent.setup();
  renderView();
  await screen.findByRole('button', { name: 'Brave Search' }, { timeout: 5000 });
  await user.type(screen.getByRole('textbox', { name: 'Search servers' }), 'brave');
  await waitFor(() => expect(forkMcpMarket.list).toHaveBeenCalledWith('official', expect.objectContaining({ q: 'brave', sort: '' })));
  expect(forkMcpMarket.list).not.toHaveBeenCalledWith('official', expect.objectContaining({ q: 'b' }));
});

it('shows bulk publishers when asked', async () => {
  const user = userEvent.setup();
  renderView();
  await screen.findByText(/120 servers from bulk publishers \(50\+ entries\) are hidden/);
  await user.click(screen.getByRole('button', { name: 'Show them' }));
  await waitFor(() => expect(forkMcpMarket.list).toHaveBeenCalledWith('official', expect.objectContaining({ bulk: true })));
});

it('says when Docker is missing on the Docker tab', async () => {
  const user = userEvent.setup();
  renderView();
  await screen.findByRole('button', { name: 'Brave Search' }, { timeout: 5000 });
  await user.click(screen.getByRole('button', { name: 'Docker' }));
  await waitFor(() => expect(forkMcpMarket.list).toHaveBeenCalledWith('docker', expect.objectContaining({ sort: 'pulls', bulk: false })));
  expect(await screen.findByText(/Docker was not found on this computer. Container servers can still be added/)).toBeInTheDocument();
  const r = row('Brave Search');
  expect(within(r).getByText('Docker not found')).toBeInTheDocument();
  expect(within(r).getByText('Built by Docker')).toBeInTheDocument();
  expect(within(r).getByText('12,000 pulls')).toBeInTheDocument();
  expect(screen.queryByText('Show bulk publishers')).toBeNull();
});
