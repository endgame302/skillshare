// Fork-only: plain GitHub repos as hubs (endgame302/skillshare).
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { beforeEach, expect, it, vi } from 'vitest';
import { I18nProvider, messagesByLocale } from '../i18n';
import { api } from '../api/client';
import { hubDrafts } from '../api/hubDrafts';
import { forkPlainHub } from '../api/forkPlainHub';
import HubPage from './HubPage';

vi.mock('../api/hubDrafts', async (original) => ({
  ...await original<typeof import('../api/hubDrafts')>(),
  hubDrafts: { list: vi.fn(), get: vi.fn() },
}));
vi.mock('../api/forkPlainHub', () => ({ forkPlainHub: { info: vi.fn() } }));
vi.mock('../api/client', async (original) => {
  const mod = await original<typeof import('../api/client')>();
  return { ...mod, api: { ...mod.api, getHubConfig: vi.fn(), putHubConfig: vi.fn(), searchHub: vi.fn(), listSkills: vi.fn() } };
});

const REPO = 'anthropics/skills';

function renderPage() {
  const router = createMemoryRouter([{ path: '*', element: <I18nProvider><HubPage /></I18nProvider> }], { initialEntries: ['/hubs'] });
  return render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><RouterProvider router={router} /></QueryClientProvider>);
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(hubDrafts.list).mockResolvedValue([]);
  vi.mocked(api.getHubConfig).mockResolvedValue({ hubs: [{ label: 'skills', url: REPO }], default: '' });
  vi.mocked(api.putHubConfig).mockResolvedValue({ success: true } as Awaited<ReturnType<typeof api.putHubConfig>>);
  vi.mocked(api.listSkills).mockResolvedValue({ resources: [] } as unknown as Awaited<ReturnType<typeof api.listSkills>>);
  vi.mocked(api.searchHub).mockImplementation(async (_q, url) => ({
    results: url === REPO
      ? [{ name: 'pdf', description: 'Read and fill PDF forms', source: 'github.com/anthropics/skills/skills/pdf', stars: 0, owner: 'anthropics', repo: 'skills' }]
      : [],
  }));
  vi.mocked(forkPlainHub.info).mockResolvedValue({
    plain: true, repo: REPO, commit: 'abc1234', listed: 500, total: 812, capped: true,
  });
});

const row = (name: RegExp) => screen.getAllByRole('button', { name }).find((b) => b.hasAttribute('aria-current'))!;

it('tags a repo hub as indexed automatically, and no hub index file', async () => {
  renderPage();
  await screen.findByText(REPO);
  expect(within(row(/anthropics\/skills/)).getByText('Auto-indexed')).toBeInTheDocument();
  expect(within(row(/Skillshare Hub/)).queryByText('Auto-indexed')).toBeNull();
});

it('lists a repo hub with where its list came from and the cap', async () => {
  const user = userEvent.setup();
  renderPage();
  await user.click(await screen.findByRole('button', { name: /anthropics\/skills/ }));
  expect(await screen.findByText('pdf')).toBeInTheDocument();
  expect(await screen.findByText(/Indexed automatically from anthropics\/skills at commit abc1234/)).toBeInTheDocument();
  expect(screen.getByText(/Only the first 500 of 812 skills are listed/)).toBeInTheDocument();
  expect(forkPlainHub.info).toHaveBeenCalledWith(REPO);
});

it('leaves a repo that cannot be read to the error', async () => {
  vi.mocked(api.searchHub).mockRejectedValue(new Error('could not read GitHub repo anthropics/skills'));
  const user = userEvent.setup();
  renderPage();
  await user.click(await screen.findByRole('button', { name: /anthropics\/skills/ }));
  expect(await screen.findByText('could not read GitHub repo anthropics/skills')).toBeInTheDocument();
  expect(screen.queryByText(/Indexed automatically/)).toBeNull();
  expect(forkPlainHub.info).not.toHaveBeenCalled();
});

it('says nothing extra on a hub index file', async () => {
  vi.mocked(api.getHubConfig).mockResolvedValue({ hubs: [], default: '' });
  renderPage();
  await screen.findByText('Skillshare Hub', { selector: 'h2' });
  expect(screen.queryByText(/Indexed automatically/)).toBeNull();
  expect(forkPlainHub.info).not.toHaveBeenCalled();
});

it('saves owner/repo typed into Add hub as it is', async () => {
  vi.mocked(api.getHubConfig).mockResolvedValue({ hubs: [], default: '' });
  const user = userEvent.setup();
  renderPage();
  await user.click(await screen.findByRole('button', { name: 'Add or create a Hub' }));
  expect(screen.getByText(/or a GitHub repo of skills/)).toBeInTheDocument();
  await user.type(screen.getByLabelText('Add an existing Hub'), 'anthropics/skills');
  await user.click(screen.getByRole('button', { name: 'Add' }));
  expect(api.putHubConfig).toHaveBeenCalledWith({ hubs: [{ label: 'skills', url: REPO }], default: '' });
});

it('keeps the add-hub hint translated', () => {
  expect(messagesByLocale.de['hubs.add.hint']).toContain('GitHub-Repository');
  expect(messagesByLocale.ja['plainhub.tag']).toBe('自動インデックス');
});
