// Fork-only (endgame302/skillshare): the skills.sh marketplace view.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { api, ApiError } from '../../../../api/client';
import { forkSkillssh, type SkillsshBoard, type SkillsshSkill } from '../../../../api/forkSkillssh';
import { I18nProvider } from '../../../../i18n';
import { ToastProvider } from '../../../Toast';
import SkillsshView from './SkillsshView';

vi.mock('../../../../api/forkSkillssh', async (original) => ({
  ...(await original<typeof import('../../../../api/forkSkillssh')>()),
  forkSkillssh: { board: vi.fn(), search: vi.fn(), resolve: vi.fn() },
}));
vi.mock('../../../../api/client', async (original) => {
  const mod = await original<typeof import('../../../../api/client')>();
  return { ...mod, api: { ...mod.api, installBatch: vi.fn(), listSkills: vi.fn() } };
});
vi.mock('../../../InstallDialog', () => ({
  default: ({ initialSource }: { initialSource?: string }) => <div role="dialog">install dialog for {initialSource}</div>,
}));

const entry = (source: string, skillId: string, installs = 1000): SkillsshSkill => ({
  id: `${source}/${skillId}`,
  source,
  skillId,
  name: skillId,
  installs,
  url: `https://skills.sh/${source}/${skillId}`,
  installable: !source.includes('.'),
});

const boards: Record<SkillsshBoard, SkillsshSkill[]> = {
  all: [entry('vercel-labs/agent-skills', 'vercel-react-best-practices', 900588), entry('open.feishu.cn', 'feishu-docs')],
  trending: [entry('designed-by-ai/skills', 'design-mobile-apps')],
  hot: [entry('lllllllama/rigorpilot-skills', 'explore-run')],
};

const list = (skills: SkillsshSkill[]) => ({ skills, fetchedAt: '2026-11-15T10:00:00Z' });

function renderView() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <I18nProvider>
        <ToastProvider>
          <SkillsshView />
        </ToastProvider>
      </I18nProvider>
    </QueryClientProvider>,
  );
}

const pause = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(forkSkillssh.board).mockImplementation(async (name) => list(boards[name]));
  vi.mocked(forkSkillssh.search).mockImplementation(async (q) => list([entry('someone/react-kit', `${q}-helper`)]));
  vi.mocked(api.listSkills).mockResolvedValue({ resources: [] });
});

describe('SkillsshView', () => {
  it('shows the All time list and reads another list when its tab is picked', async () => {
    const user = userEvent.setup();
    renderView();
    expect(await screen.findByText('vercel-react-best-practices')).toBeInTheDocument();
    expect(forkSkillssh.board).toHaveBeenCalledWith('all');
    // A skill published from a website has no repo to install from.
    expect(screen.queryByRole('button', { name: 'Install feishu-docs' })).toBeNull();

    await user.click(screen.getByRole('tab', { name: 'Trending' }));
    expect(await screen.findByText('design-mobile-apps')).toBeInTheDocument();
    expect(forkSkillssh.board).toHaveBeenLastCalledWith('trending');
    expect(screen.getByRole('tab', { name: 'Trending' })).toHaveAttribute('aria-selected', 'true');

    await user.click(screen.getByRole('tab', { name: 'Hot' }));
    expect(await screen.findByText('explore-run')).toBeInTheDocument();
    expect(forkSkillssh.board).toHaveBeenLastCalledWith('hot');
  });

  it('searches skills.sh only from two characters', async () => {
    const user = userEvent.setup();
    renderView();
    await screen.findByText('vercel-react-best-practices');
    const box = screen.getByRole('textbox', { name: 'Search skills.sh' });

    await user.type(box, 'r');
    await pause(500);
    expect(forkSkillssh.search).not.toHaveBeenCalled();
    expect(screen.getByText('Type at least 2 characters to search skills.sh.')).toBeInTheDocument();
    expect(screen.getByText('vercel-react-best-practices')).toBeInTheDocument();

    await user.type(box, 'e');
    expect(await screen.findByText('re-helper')).toBeInTheDocument();
    expect(forkSkillssh.search).toHaveBeenCalledTimes(1);
    expect(forkSkillssh.search).toHaveBeenCalledWith('re');
    expect(screen.getAllByRole('tab').every((tab) => tab.getAttribute('aria-selected') === 'false')).toBe(true);

    // Picking a list leaves the search.
    await user.click(screen.getByRole('tab', { name: 'All time' }));
    expect(box).toHaveValue('');
    expect(await screen.findByText('vercel-react-best-practices')).toBeInTheDocument();
  });

  it('says calmly when a list cannot be read, and search still works', async () => {
    vi.mocked(forkSkillssh.board).mockImplementation(async (name) => {
      if (name === 'trending') throw new ApiError(502, 'skills.sh format changed: no skills found', { code: 'skillssh.format_changed' });
      return list(boards[name]);
    });
    const user = userEvent.setup();
    renderView();
    await screen.findByText('vercel-react-best-practices');

    await user.click(screen.getByRole('tab', { name: 'Trending' }));
    const note = await screen.findByRole('status');
    expect(note).toHaveTextContent("Couldn't read skills.sh's Trending list — it may have changed. Search still works.");
    expect(within(note).getByRole('link', { name: 'Open on skills.sh' })).toHaveAttribute('href', 'https://skills.sh/trending');
    expect(within(note).queryByRole('button', { name: 'Try again' })).toBeNull();

    await user.type(screen.getByRole('textbox', { name: 'Search skills.sh' }), 'react');
    expect(await screen.findByText('react-helper')).toBeInTheDocument();
  });

  it('installs the one skill an entry names, from the folder its repo keeps it in', async () => {
    const skill = { name: 'react-best-practices', path: 'skills/react-best-practices', description: 'React rules', kind: 'skill' as const };
    vi.mocked(forkSkillssh.resolve).mockResolvedValue({ skill });
    vi.mocked(api.installBatch).mockResolvedValue({ results: [{ name: 'react-best-practices', action: 'installed' }], summary: 'Installed 1 skill' });
    const user = userEvent.setup();
    renderView();

    await user.click(await screen.findByRole('button', { name: 'Install vercel-react-best-practices' }));
    await waitFor(() => expect(api.installBatch).toHaveBeenCalledTimes(1));
    expect(forkSkillssh.resolve).toHaveBeenCalledWith('vercel-labs/agent-skills', 'vercel-react-best-practices');
    expect(api.installBatch).toHaveBeenCalledWith({ source: 'vercel-labs/agent-skills', skills: [skill] });
    expect(await screen.findByText('Installed')).toBeInTheDocument();
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('hands over to the install dialog when the repo has no skill by that id', async () => {
    vi.mocked(forkSkillssh.resolve).mockRejectedValue(
      new ApiError(404, 'no skill named vercel-react-best-practices', { code: 'skillssh.skill_not_found' }),
    );
    const user = userEvent.setup();
    renderView();

    await user.click(await screen.findByRole('button', { name: 'Install vercel-react-best-practices' }));
    expect(await screen.findByRole('dialog')).toHaveTextContent('install dialog for vercel-labs/agent-skills');
    expect(api.installBatch).not.toHaveBeenCalled();
  });

  it('hands an install the audit blocked to the install dialog', async () => {
    vi.mocked(forkSkillssh.resolve).mockResolvedValue({ skill: { name: 'x', path: 'x', kind: 'skill' } });
    vi.mocked(api.installBatch).mockResolvedValue({
      results: [{ name: 'x', error: 'security audit failed: findings at/above HIGH' }],
      summary: '',
    });
    const user = userEvent.setup();
    renderView();

    await user.click(await screen.findByRole('button', { name: 'Install vercel-react-best-practices' }));
    expect(await screen.findByRole('dialog')).toHaveTextContent('install dialog for vercel-labs/agent-skills');
    expect(screen.queryByText('Installed')).toBeNull();
  });
});
