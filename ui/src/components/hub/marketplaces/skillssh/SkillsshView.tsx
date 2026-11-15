// Fork-only (endgame302/skillshare): browse skills.sh on the Hubs page.
import { useEffect, useMemo, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { AlertTriangle, ExternalLink, Info, RefreshCw, Search, X } from 'lucide-react';
import { api, ApiError } from '../../../../api/client';
import {
  forkSkillssh,
  SKILLSSH_BOARD_URL,
  SKILLSSH_BOARDS,
  SKILLSSH_ERROR,
  SKILLSSH_MIN_QUERY,
  SKILLSSH_URL,
  skillsshKeys,
  type SkillsshBoard,
  type SkillsshSkill,
} from '../../../../api/forkSkillssh';
import { useSkillsQuery } from '../../../../hooks/useSharedQueries';
import { useI18n, useT } from '../../../../i18n';
import { clearAuditCache } from '../../../../lib/auditCache';
import { isAuditBlock } from '../../../../lib/auditMessage';
import { invalidate } from '../../../../lib/queryEvents';
import Button from '../../../Button';
import EmptyState from '../../../EmptyState';
import InstallDialog from '../../../InstallDialog';
import Spinner from '../../../Spinner';
import { useToast } from '../../../Toast';
import { installedIds } from './skillssh';

const PAGE = 50;
const SEARCH_DELAY_MS = 350;
const LIST_STALE_MS = 10 * 60_000;

const BOARD_LABEL: Record<SkillsshBoard, string> = {
  all: 'hubs.skillssh.board.all',
  trending: 'hubs.skillssh.board.trending',
  hot: 'hubs.skillssh.board.hot',
};
const BOARD_CAPTION: Record<SkillsshBoard, string> = {
  all: 'hubs.skillssh.caption.all',
  trending: 'hubs.skillssh.caption.trending',
  hot: 'hubs.skillssh.caption.hot',
};

const errorCode = (e: unknown) => (e instanceof ApiError ? e.code : undefined);

/**
 * The skills.sh leaderboards (All time, Trending, Hot) and its search. Installing resolves the
 * entry's skill id to a folder in its GitHub repo and installs that one skill through the normal
 * batch install, so the audit gate applies; anything that needs a choice opens the install dialog.
 */
export default function SkillsshView() {
  const t = useT();
  const { locale } = useI18n();
  const { toast } = useToast();
  const queryClient = useQueryClient();
  const [board, setBoard] = useState<SkillsshBoard>('all');
  const [query, setQuery] = useState('');
  const [term, setTerm] = useState('');
  const [shown, setShown] = useState(PAGE);
  const [busy, setBusy] = useState<string | null>(null);
  const [justInstalled, setJustInstalled] = useState<Set<string>>(new Set());
  const [dialogSource, setDialogSource] = useState<string | null>(null);

  const typed = query.trim();
  useEffect(() => {
    const id = setTimeout(() => setTerm(typed.length >= SKILLSSH_MIN_QUERY ? typed : ''), SEARCH_DELAY_MS);
    return () => clearTimeout(id);
  }, [typed]);
  const searching = term !== '';
  useEffect(() => setShown(PAGE), [board, term]);

  const list = useQuery({
    queryKey: skillsshKeys.board(board),
    queryFn: () => forkSkillssh.board(board),
    staleTime: LIST_STALE_MS,
    retry: false,
  });
  const search = useQuery({
    queryKey: skillsshKeys.search(term),
    queryFn: () => forkSkillssh.search(term),
    enabled: searching,
    staleTime: LIST_STALE_MS,
    retry: false,
  });
  const current = searching ? search : list;
  const skills = current.data?.skills;

  const { data: skillsData } = useSkillsQuery();
  const installed = useMemo(() => {
    const ids = installedIds(skillsData?.resources ?? [], skills ?? []);
    for (const id of justInstalled) ids.add(id);
    return ids;
  }, [skillsData, skills, justInstalled]);

  const compact = useMemo(() => new Intl.NumberFormat(locale, { notation: 'compact', maximumFractionDigits: 1 }), [locale]);
  const exact = useMemo(() => new Intl.NumberFormat(locale), [locale]);
  const time = useMemo(() => new Intl.DateTimeFormat(locale, { timeStyle: 'short' }), [locale]);
  const boardName = t(BOARD_LABEL[board]);

  const pickBoard = (next: SkillsshBoard) => {
    setBoard(next);
    setQuery('');
    setTerm('');
  };

  const install = async (s: SkillsshSkill) => {
    setBusy(s.id);
    try {
      const { skill } = await forkSkillssh.resolve(s.source, s.skillId);
      const res = await api.installBatch({ source: s.source, skills: [skill] });
      const item = res.results.find((r) => r.name === skill.name) ?? res.results[0];
      if (item?.error && isAuditBlock(item.error)) {
        // The dialog shows the findings and offers to install anyway.
        toast(t('hubs.skillssh.install.blocked', { name: s.name }), 'warning');
        setDialogSource(s.source);
      } else if (item?.action === 'skipped' || item?.error?.startsWith('already exists')) {
        toast(t('hubs.skillssh.install.exists', { name: s.name }), 'warning');
      } else if (item?.error) {
        toast(item.error, 'error');
      } else {
        toast(t(item?.warnings?.length ? 'hubs.skillssh.install.doneWarnings' : 'hubs.skillssh.install.done', { name: s.name }), item?.warnings?.length ? 'warning' : 'success');
        setJustInstalled((prev) => new Set(prev).add(s.id));
        clearAuditCache(queryClient);
        void invalidate(queryClient, 'skillsChanged');
      }
    } catch (e) {
      if (errorCode(e) === SKILLSSH_ERROR.notFound) {
        toast(t('hubs.skillssh.install.notFound', { skill: s.skillId, source: s.source }), 'info');
        setDialogSource(s.source);
      } else {
        toast((e as Error).message, 'error');
      }
    } finally {
      setBusy(null);
    }
  };

  const openSite = (href: string) => (
    <a href={href} target="_blank" rel="noreferrer" className="ss-more inline-flex shrink-0 items-center gap-1">
      {t('hubs.skillssh.openSite')}
      <ExternalLink size={12} aria-hidden="true" />
    </a>
  );

  let body;
  if (current.isPending) {
    body = (
      <div className="ss-box flex items-center gap-2.5 text-[13px] text-ink-2">
        <Spinner size="sm" />
        {t(searching ? 'hubs.skillssh.searching' : 'hubs.skillssh.loading')}
      </div>
    );
  } else if (current.isError) {
    const changed = errorCode(current.error) === SKILLSSH_ERROR.formatChanged;
    body = (
      <div role="status" className={`ss-note ${changed ? 'inf' : 'warn'}`}>
        {changed ? <Info size={16} /> : <AlertTriangle size={16} />}
        <span className="flex flex-1 flex-col gap-1.5">
          <span>
            {searching
              ? t('hubs.skillssh.searchFailed')
              : changed
                ? t('hubs.skillssh.formatChanged', { list: boardName })
                : t('hubs.skillssh.unreachable', { list: boardName })}
          </span>
          {/* What went wrong, as the server put it; the sentence above already says it calmly. */}
          {!changed && <span className="break-words font-mono text-[11.5px] text-ink-3">{(current.error as Error).message}</span>}
          <span className="flex items-center gap-4">
            {!changed && (
              <button type="button" className="ss-more" onClick={() => void current.refetch()}>
                {t('hubs.skillssh.retry')}
              </button>
            )}
            {openSite(searching ? SKILLSSH_URL : SKILLSSH_BOARD_URL[board])}
          </span>
        </span>
      </div>
    );
  } else if (skills && skills.length === 0) {
    body = <EmptyState icon={Search} title={searching ? t('hubs.skillssh.noResults', { query: term }) : t('hubs.skillssh.empty', { list: boardName })} />;
  } else if (skills) {
    body = (
      <>
        <div className="ss-list">
          <div className="ss-lh">
            <span className="w-8 shrink-0 text-end">#</span>
            <span className="flex-1">{t('hubs.skillssh.col.skill')}</span>
            <span className="w-[84px] shrink-0 text-end">{t('hubs.skillssh.col.installs')}</span>
            <span className="w-[112px] shrink-0" />
          </div>
          {skills.slice(0, shown).map((s, i) => (
            <div key={s.id} className="ss-r">
              <span className="w-8 shrink-0 text-end text-[12.5px] tabular-nums text-ink-3">{i + 1}</span>
              <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                <span className="flex min-w-0 items-center gap-1.5">
                  <span className="nm m truncate font-mono">{s.name}</span>
                  <a href={s.url} target="_blank" rel="noreferrer" className="inline-flex size-6 shrink-0 items-center justify-center text-ink-3 hover:text-ink" aria-label={t('hubs.skillssh.openSkill', { name: s.name })} title={t('hubs.skillssh.openSkill', { name: s.name })}>
                    <ExternalLink size={12} />
                  </a>
                </span>
                <span className="truncate font-mono text-[11.5px] text-ink-3">{s.source}</span>
              </span>
              <span className="w-[84px] shrink-0 text-end text-[13px] tabular-nums text-ink-2" title={t('hubs.skillssh.installsExact', { count: exact.format(s.installs) })}>
                {compact.format(s.installs)}
              </span>
              <span className="flex w-[112px] shrink-0 justify-end">
                {installed.has(s.id) ? (
                  <span className="ss-st ok">{t('hubs.browse.installed')}</span>
                ) : !s.installable ? (
                  <span className="text-end text-[12px] text-ink-3" title={t('hubs.skillssh.notGithubHint', { source: s.source })}>{t('hubs.skillssh.notGithub')}</span>
                ) : (
                  <Button variant="secondary" size="sm" loading={busy === s.id} disabled={busy !== null && busy !== s.id} onClick={() => void install(s)} aria-label={t('hubs.skillssh.installNamed', { name: s.name })}>
                    {t('hubs.browse.install')}
                  </Button>
                )}
              </span>
            </div>
          ))}
        </div>
        <div className="flex flex-wrap items-center gap-3 text-[12px] text-ink-3">
          {shown < skills.length && (
            <Button variant="ghost" size="sm" onClick={() => setShown(shown + PAGE)}>
              {t('resources.showMore')}
            </Button>
          )}
          {current.data && <span>{t('hubs.skillssh.fetchedAt', { time: time.format(new Date(current.data.fetchedAt)) })}</span>}
        </div>
      </>
    );
  }

  return (
    <div className="flex min-w-0 flex-col gap-3">
      <div className="ss-sec !mb-0 !items-center">
        <h2>{t('hubs.skillssh.title')}</h2>
        {skills && <span className="ss-cnt">{skills.length}</span>}
        <span className="ml-auto flex items-center gap-2.5">
          <span className="ss-inp sm !h-8 w-[240px]">
            <Search size={14} className="shrink-0 text-ink-3" />
            <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder={t('hubs.skillssh.search')} aria-label={t('hubs.skillssh.search')} />
            {query && (
              <button type="button" className="inline-flex size-6 shrink-0 items-center justify-center text-ink-3" aria-label={t('hubs.skillssh.clearSearch')} onClick={() => setQuery('')}>
                <X size={13} />
              </button>
            )}
          </span>
          {!searching && (
            <button
              type="button"
              className="ss-btn !w-[34px] !px-0"
              aria-label={t('hubs.skillssh.refresh')}
              title={t('hubs.skillssh.refresh')}
              disabled={list.isFetching}
              onClick={() => void list.refetch()}
            >
              <RefreshCw size={15} className={list.isFetching ? 'animate-spin' : ''} />
            </button>
          )}
        </span>
      </div>

      <div className="ss-tabs" role="tablist" aria-label={t('hubs.skillssh.title')}>
        {SKILLSSH_BOARDS.map((b) => (
          <button key={b} type="button" role="tab" aria-selected={!searching && board === b} className={!searching && board === b ? 'on' : ''} onClick={() => pickBoard(b)}>
            {t(BOARD_LABEL[b])}
          </button>
        ))}
      </div>
      <span className="flex flex-wrap items-center gap-x-4 gap-y-1 text-[12.5px] text-ink-3">
        <span>
          {searching
            ? t('hubs.skillssh.results', { query: term })
            : typed.length > 0 && typed.length < SKILLSSH_MIN_QUERY
              ? t('hubs.skillssh.minQuery', { count: SKILLSSH_MIN_QUERY })
              : t(BOARD_CAPTION[board])}
        </span>
        {!searching && current.data?.stale && <span className="text-warn">{t('hubs.skillssh.stale', { time: time.format(new Date(current.data.fetchedAt)) })}</span>}
      </span>

      {body}

      {dialogSource && (
        <InstallDialog
          kind="skill"
          initialTab="url"
          initialSource={dialogSource}
          onClose={() => {
            setDialogSource(null);
            void invalidate(queryClient, 'installDialogClosed');
          }}
        />
      )}
    </div>
  );
}
