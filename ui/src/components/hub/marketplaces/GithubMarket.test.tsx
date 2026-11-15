// Fork-only: the GitHub marketplace on the Hubs page (endgame302/skillshare).
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, expect, it, vi } from 'vitest';
import { api, ApiError } from '../../../api/client';
import type { SearchResult } from '../../../api/client';
import { I18nProvider } from '../../../i18n';
import { ToastProvider } from '../../Toast';
import GithubMarket from './GithubMarket';
import { marketplace } from './github.market';
import { MARKETPLACES } from './registry';

vi.mock('../../../api/client', async (original) => {
  const mod = await original<typeof import('../../../api/client')>();
  return {
    ...mod,
    api: { ...mod.api, search: vi.fn(), preview: vi.fn(), listSkills: vi.fn(), getHubConfig: vi.fn(), discover: vi.fn(), installBatch: vi.fn() },
  };
});

const result = (name: string, source: string, stars = 0): SearchResult => ({
  name, source, stars, description: `${name} skill`, owner: source.split('/')[0], repo: source.split('/')[1],
});
const PDF = result('pdf', 'anthropics/skills/skills/pdf', 41200);
const DOCX = result('docx', 'anthropics/skills/skills/docx', 41200);

function renderView() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <I18nProvider>
          <ToastProvider>
            <GithubMarket />
          </ToastProvider>
        </I18nProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

async function searchFor(user: ReturnType<typeof userEvent.setup>, text: string) {
  await user.type(screen.getByRole('textbox', { name: /search github/i }), `${text}{Enter}`);
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(api.listSkills).mockResolvedValue({ resources: [] });
  vi.mocked(api.getHubConfig).mockResolvedValue({ hubs: [], default: '' });
  vi.mocked(api.search).mockResolvedValue({ results: [PDF, DOCX] });
});

it('registers itself as the first marketplace', () => {
  expect(MARKETPLACES).toContain(marketplace);
  expect(marketplace).toMatchObject({ id: 'github', order: 10 });
});

it('searches GitHub for what was typed and marks skills already installed', async () => {
  vi.mocked(api.listSkills).mockResolvedValue({
    resources: [{ name: 'docx', kind: 'skill', flatName: 'docx', relPath: 'docx', sourcePath: '/s/docx', isInRepo: false, source: 'github.com/anthropics/skills/skills/docx' }],
  });
  const user = userEvent.setup();
  renderView();

  await searchFor(user, '  pdf ');

  expect(api.search).toHaveBeenCalledWith('pdf', 50);
  expect(await screen.findByText('anthropics/skills/skills/pdf')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Install pdf' })).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Install docx' })).not.toBeInTheDocument();
});

it('says plainly when GitHub rate-limits the search, and tries the same search again', async () => {
  vi.mocked(api.search).mockRejectedValueOnce(
    new ApiError(500, "GitHub API rate limit exceeded (0/60 remaining); set GITHUB_TOKEN/GH_TOKEN or run 'gh auth login'"),
  );
  const user = userEvent.setup();
  renderView();

  await searchFor(user, 'pdf');

  const alert = await screen.findByRole('alert');
  expect(alert).toHaveTextContent("GitHub's rate limit was reached");
  expect(alert).toHaveTextContent('GITHUB_TOKEN');
  await user.click(within(alert).getByRole('button', { name: 'Try again' }));
  await waitFor(() => expect(api.search).toHaveBeenCalledTimes(2));
  expect(api.search).toHaveBeenLastCalledWith('pdf', 50);
  expect(await screen.findByText('anthropics/skills/skills/pdf')).toBeInTheDocument();
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
});

it('shows any other failure with GitHub’s own message', async () => {
  vi.mocked(api.search).mockRejectedValueOnce(new ApiError(500, 'github search failed: 422 Validation Failed'));
  const user = userEvent.setup();
  renderView();

  await searchFor(user, 'pdf');

  expect(await screen.findByRole('alert')).toHaveTextContent('422 Validation Failed');
});

it('installs a result through the install dialog, so the audit runs', async () => {
  vi.mocked(api.discover).mockResolvedValue({ needsSelection: false, skills: [{ name: 'pdf', path: '.' }], agents: [] });
  vi.mocked(api.installBatch).mockResolvedValue({ results: [{ name: 'pdf', action: 'installed' }], summary: 'Installed 1' });
  const user = userEvent.setup();
  renderView();

  await searchFor(user, 'pdf');
  await user.click(await screen.findByRole('button', { name: 'Install pdf' }));

  expect(await screen.findByLabelText(/git url/i)).toHaveValue('anthropics/skills/skills/pdf');
  await user.click(screen.getByRole('button', { name: /find skills/i }));
  await user.click(await screen.findByRole('button', { name: /install 1 skill$/i }));
  await waitFor(() => expect(api.installBatch).toHaveBeenCalledWith(
    expect.objectContaining({ source: 'anthropics/skills/skills/pdf', skills: [expect.objectContaining({ name: 'pdf' })] }),
  ));
});

it('previews a result’s SKILL.md and installs from the preview', async () => {
  vi.mocked(api.preview).mockResolvedValue({
    name: 'pdf', description: 'Work with PDFs', content: '---\nname: pdf\n---\n# Reading PDFs\n', source: PDF.source, stars: PDF.stars, owner: 'anthropics', repo: 'skills',
  });
  const user = userEvent.setup();
  renderView();

  await searchFor(user, 'pdf');
  await user.click(await screen.findByText('anthropics/skills/skills/pdf'));

  const dialog = await screen.findByRole('dialog', { name: 'Preview' });
  expect(api.preview).toHaveBeenCalledWith('anthropics/skills/skills/pdf');
  expect(await within(dialog).findByRole('heading', { name: 'Reading PDFs' })).toBeInTheDocument();
  expect(within(dialog).getByRole('link', { name: /open on github/i })).toHaveAttribute('href', 'https://github.com/anthropics/skills/tree/HEAD/skills/pdf');
  await user.click(within(dialog).getByRole('button', { name: 'Install' }));
  expect(await screen.findByLabelText(/git url/i)).toHaveValue('anthropics/skills/skills/pdf');
  expect(screen.queryByRole('dialog', { name: 'Preview' })).not.toBeInTheDocument();
});
