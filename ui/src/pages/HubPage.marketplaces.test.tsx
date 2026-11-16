// Fork-only: the Marketplaces slot under the hub list (endgame302/skillshare).
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { Globe } from 'lucide-react';
import { beforeEach, expect, it, vi } from 'vitest';
import { I18nProvider, messagesByLocale } from '../i18n';
import { api } from '../api/client';
import { hubDrafts } from '../api/hubDrafts';
import HubPage from './HubPage';

vi.mock('../api/hubDrafts', async (original) => ({
  ...await original<typeof import('../api/hubDrafts')>(),
  hubDrafts: { list: vi.fn(), get: vi.fn() },
}));
vi.mock('../api/client', async (original) => {
  const mod = await original<typeof import('../api/client')>();
  return { ...mod, api: { ...mod.api, getHubConfig: vi.fn(), searchHub: vi.fn(), listSkills: vi.fn() } };
});
vi.mock('../components/hub/marketplaces/registry', async (original) => {
  const mod = await original<typeof import('../components/hub/marketplaces/registry')>();
  const fake = { id: 'fake', order: 1, labelKey: 'hubs.market.title', subKey: 'hubs.market.title', icon: Globe, View: () => <p>fake marketplace view</p> };
  return { ...mod, MARKETPLACES: [fake], findMarketplace: (key: string | null) => (key === mod.MARKET + 'fake' ? fake : undefined) };
});

function renderPage() {
  const router = createMemoryRouter([{ path: '*', element: <I18nProvider><HubPage /></I18nProvider> }], { initialEntries: ['/hubs'] });
  return render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><RouterProvider router={router} /></QueryClientProvider>);
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(hubDrafts.list).mockResolvedValue([]);
  vi.mocked(api.getHubConfig).mockResolvedValue({ hubs: [], default: '' });
  vi.mocked(api.searchHub).mockResolvedValue({ results: [] });
  vi.mocked(api.listSkills).mockResolvedValue({ resources: [] } as unknown as Awaited<ReturnType<typeof api.listSkills>>);
});

it('lists marketplaces under the hubs and shows the picked one', async () => {
  const user = userEvent.setup(); renderPage();
  expect(await screen.findByRole('heading', { name: 'Marketplaces' })).toBeInTheDocument();
  const row = screen.getAllByRole('button', { name: /Marketplaces/ }).find((b) => b.getAttribute('aria-current') !== null)!;
  expect(row).toHaveAttribute('aria-current', 'false');
  await user.click(row);
  expect(await screen.findByText('fake marketplace view')).toBeInTheDocument();
  expect(row).toHaveAttribute('aria-current', 'true');
});

it('gives fork strings to every locale, in its own language where a file translates them', () => {
  // Every fork file is translated now; a locale a file leaves out falls back to its English,
  // and the parity tests in src/i18n fail if any locale misses a fork key.
  expect(messagesByLocale.en['hubs.market.title']).toBe('Marketplaces');
  expect(messagesByLocale.de['hubs.market.title']).toBe('Marktplätze');
  expect(messagesByLocale['zh-TW']['clawhub.onGitHub']).toBe('在 GitHub 上');
});
