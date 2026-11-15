// Fork-only (endgame302/skillshare): search GitHub for skills from the Hubs page.
import { useMemo, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { CircleX, Clock, Download, ExternalLink, Github, Info, KeyRound, Puzzle, Search, ShieldCheck, Star, X } from 'lucide-react';
import { api } from '../../../api/client';
import type { SearchResult } from '../../../api/client';
import Button from '../../Button';
import CodeView from '../../CodeView';
import DialogShell from '../../DialogShell';
import EmptyState from '../../EmptyState';
import InstallDialog from '../../InstallDialog';
import MarkdownView, { ViewToggle } from '../../MarkdownView';
import Spinner from '../../Spinner';
import { useSkillsQuery } from '../../../hooks/useSharedQueries';
import { useI18n, useT } from '../../../i18n';
import { parseSkillMarkdown } from '../../../lib/frontmatter';
import { invalidate } from '../../../lib/queryEvents';
import { queryKeys, staleTimes } from '../../../lib/queryKeys';
import { githubURL, installedSources, searchProblem, sourceKey } from './githubSearch';
import type { SearchProblem } from './githubSearch';

/** The server ranks every match before it cuts to the limit, so asking for more costs nothing extra. */
const LIMIT = 50;
const STEP = 20;

/** Star counts as "12.3K", in the reader's locale. */
function useCompact() {
  const { locale } = useI18n();
  return useMemo(() => new Intl.NumberFormat(locale, { notation: 'compact', maximumFractionDigits: 1 }), [locale]);
}

const PROBLEM: Record<SearchProblem, { note: string; icon: typeof Clock; title: string; hint?: string }> = {
  rateLimit: { note: 'warn', icon: Clock, title: 'hubs.market.github.rateLimit', hint: 'hubs.market.github.rateLimitHint' },
  auth: { note: 'inf', icon: KeyRound, title: 'hubs.market.github.token', hint: 'hubs.market.github.tokenHint' },
  // GitHub's own message says more than any general text could.
  other: { note: 'bad', icon: CircleX, title: 'hubs.market.github.failed' },
};

/**
 * GitHub code search for SKILL.md files, the same search the install dialog runs. Installing
 * hands the source to the install dialog, as hub rows do, so the audit gate stays in one place.
 */
export default function GithubMarket() {
  const t = useT();
  const queryClient = useQueryClient();
  const [query, setQuery] = useState('');
  const [searched, setSearched] = useState<string | null>(null);
  const [results, setResults] = useState<SearchResult[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [searching, setSearching] = useState(false);
  const [shown, setShown] = useState(STEP);
  const [previewing, setPreviewing] = useState<SearchResult | null>(null);
  const [installing, setInstalling] = useState<string | null>(null);
  // Only the newest search may write its answer; an older one can come back later.
  const latest = useRef(0);

  const { data: skillsData } = useSkillsQuery();
  const installed = useMemo(() => installedSources(skillsData?.resources ?? []), [skillsData]);
  const isInstalled = (r: SearchResult) => installed.has(sourceKey(r.source));
  const compact = useCompact();

  const search = async (q: string) => {
    const id = ++latest.current;
    setSearching(true);
    setSearched(q);
    setError(null);
    try {
      const res = await api.search(q, LIMIT);
      if (id !== latest.current) return;
      setResults(res.results);
      setShown(STEP);
    } catch (e) {
      if (id !== latest.current) return;
      setResults(null);
      setError((e as Error).message);
    } finally {
      if (id === latest.current) setSearching(false);
    }
  };

  const submit = (e: FormEvent) => {
    e.preventDefault();
    void search(query.trim());
  };

  const install = (source: string) => {
    setPreviewing(null);
    setInstalling(source);
  };

  let content;
  if (error) {
    const p = PROBLEM[searchProblem(error)];
    const Icon = p.icon;
    content = (
      <div role="alert" className={`ss-note ${p.note}`}>
        <Icon size={16} />
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <b>{t(p.title)}</b>
          <span className="break-words">{p.hint ? t(p.hint) : error}</span>
        </div>
        <Button variant="secondary" size="sm" disabled={searching} onClick={() => void search(searched ?? '')}>
          {t('hubs.market.github.retry')}
        </Button>
      </div>
    );
  } else if (results === null) {
    content = searching ? (
      <div className="ss-box flex items-center gap-2.5 text-[13px] text-ink-2">
        <Spinner size="sm" />
        {t('hubs.market.github.searching')}
      </div>
    ) : (
      <EmptyState
        icon={Github}
        title={t('hubs.market.github.startTitle')}
        description={t('hubs.market.github.startHint')}
        action={<Button variant="secondary" onClick={() => void search('')}>{t('hubs.market.github.popular')}</Button>}
      />
    );
  } else if (results.length === 0) {
    content = <EmptyState icon={Search} title={t('hubs.market.github.noResults')} description={t('hubs.market.github.noResultsHint')} />;
  } else {
    content = (
      <>
        <div className="ss-list">
          <div className="ss-lh">
            <span className="flex-1">{t('hubs.market.github.skill')}</span>
            <span className="w-16 shrink-0">{t('hubs.market.github.stars')}</span>
            <span className="w-[96px] shrink-0" />
          </div>
          {results.slice(0, shown).map((r, i) => (
            <div key={`${i}:${r.source}`} className="ss-r">
              <button
                type="button"
                className="flex min-w-0 flex-1 flex-col gap-0.5 text-start"
                title={t('hubs.market.github.preview')}
                onClick={() => setPreviewing(r)}
              >
                <span className="nm m truncate font-mono">{r.name}</span>
                {r.description && <span className="w-full truncate text-[13px] text-ink-2">{r.description}</span>}
                <span className="w-full truncate font-mono text-[11.5px] text-ink-3">{r.source}</span>
              </button>
              <span className="flex w-16 shrink-0 items-center gap-1 text-xs text-ink-3">
                {r.stars > 0 && <><Star size={13} aria-hidden="true" />{compact.format(r.stars)}</>}
              </span>
              <span className="flex w-[96px] shrink-0 justify-end">
                {isInstalled(r) ? (
                  <span className="ss-st ok">{t('hubs.market.github.installed')}</span>
                ) : (
                  <button type="button" className="ss-btn sm" aria-label={t('hubs.market.github.installNamed', { name: r.name })} onClick={() => install(r.source)}>
                    {t('hubs.market.github.install')}
                  </button>
                )}
              </span>
            </div>
          ))}
        </div>
        <div className="flex items-center justify-between gap-3 text-[13px] text-ink-3">
          <span>{t('hubs.market.github.range', { shown: Math.min(shown, results.length), total: results.length })}</span>
          {shown < results.length && (
            <Button variant="ghost" size="sm" onClick={() => setShown(shown + STEP)}>{t('hubs.market.github.showMore')}</Button>
          )}
        </div>
      </>
    );
  }

  return (
    <div className="flex min-w-0 flex-col gap-3">
      <div className="ss-sec !mb-0 flex-wrap !items-center gap-y-2">
        <h2 className="flex items-center gap-2"><Github size={16} aria-hidden="true" />{t('hubs.market.github.label')}</h2>
        {results && <span className="ss-cnt">{results.length}</span>}
        <form role="search" className="ml-auto flex items-center gap-2" onSubmit={submit}>
          <span className="ss-inp sm !h-8 w-[280px] max-w-full">
            <Search size={14} className="shrink-0 text-ink-3" />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t('hubs.market.github.placeholder')}
              aria-label={t('hubs.market.github.placeholder')}
            />
            {searching && <Spinner size="sm" />}
          </span>
          <Button type="submit" size="sm" disabled={searching}>{t('hubs.market.github.search')}</Button>
        </form>
      </div>
      <span className="flex items-center gap-1.5 text-[12.5px] text-ink-3">
        <ShieldCheck size={13} className="shrink-0" aria-hidden="true" />
        {t('hubs.market.github.note')}
      </span>

      {content}

      {previewing && (
        <Preview result={previewing} installed={isInstalled(previewing)} onInstall={() => install(previewing.source)} onClose={() => setPreviewing(null)} />
      )}
      {installing && (
        <InstallDialog
          kind="skill"
          initialTab="url"
          initialSource={installing}
          onClose={() => {
            setInstalling(null);
            void invalidate(queryClient, 'installDialogClosed');
          }}
        />
      )}
    </div>
  );
}

/** A result's SKILL.md, read from GitHub before anything is installed. */
function Preview({ result: r, installed, onInstall, onClose }: { result: SearchResult; installed: boolean; onInstall: () => void; onClose: () => void }) {
  const t = useT();
  const compact = useCompact();
  const [raw, setRaw] = useState(false);
  const { data, error, isFetching, refetch } = useQuery({
    queryKey: queryKeys.preview(r.source),
    queryFn: () => api.preview(r.source),
    retry: false,
    staleTime: staleTimes.config,
  });
  const md = data?.content ? parseSkillMarkdown(data.content).body.trim() : '';
  const stars = data?.stars || r.stars;
  const tags = data?.tags?.length ? data.tags : r.tags;
  const url = githubURL(r);
  const title = t('hubs.market.github.previewTitle');

  return (
    <DialogShell open onClose={onClose} padding="none" ariaLabel={title} className="!max-w-[640px]">
      <div className="dh">
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <h2 className="ss-h2">{title}</h2>
          <p className="text-[13px] text-ink-2">{t('hubs.market.github.previewSub')}</p>
        </div>
        <button type="button" className="ss-ib" aria-label={t('common.close')} onClick={onClose}>
          <X size={18} />
        </button>
      </div>
      <div className="db min-h-0 flex-1 overflow-y-auto">
        <div className="flex items-center gap-3.5">
          <span className="ss-cat skill"><Puzzle size={17} /></span>
          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
            <span className="flex items-center gap-2.5">
              <span className="truncate font-mono text-[15px] font-semibold">{data?.name || r.name}</span>
              {stars > 0 && (
                <span className="flex items-center gap-1 text-xs text-ink-3">
                  <Star size={13} aria-hidden="true" />
                  {compact.format(stars)}
                </span>
              )}
            </span>
            <span className="flex min-w-0 items-center gap-1.5 text-[13px] text-ink-2">
              <Github size={13} className="shrink-0" aria-hidden="true" />
              <span className="truncate font-mono">{r.source}</span>
            </span>
          </div>
          {md && <ViewToggle raw={raw} onChange={setRaw} />}
        </div>
        {isFetching && !data ? (
          <div className="ss-box !shadow-none flex justify-center py-10"><Spinner /></div>
        ) : md ? (
          raw ? (
            <CodeView content={data?.content ?? ''} lang="md" className="max-h-[360px]" />
          ) : (
            <div className="ss-box !shadow-none max-h-[360px] overflow-y-auto">
              <MarkdownView>{md}</MarkdownView>
            </div>
          )
        ) : (
          <div className={`ss-note ${error ? 'bad' : 'inf'}`}>
            {error ? <CircleX size={16} /> : <Info size={16} />}
            <div className="flex-1 break-words">{error ? error.message : t('hubs.market.github.previewEmpty')}</div>
            {error && <Button variant="secondary" size="sm" onClick={() => void refetch()}>{t('hubs.market.github.retry')}</Button>}
          </div>
        )}
        <dl className="ss-kv">
          {(data?.description || r.description) && (
            <><dt>{t('hubs.market.github.description')}</dt><dd>{data?.description || r.description}</dd></>
          )}
          {data?.license && <><dt>{t('hubs.market.github.license')}</dt><dd>{data.license}</dd></>}
          {tags && tags.length > 0 && (
            <><dt>{t('hubs.market.github.tags')}</dt><dd className="flex flex-wrap gap-1">{tags.map((x) => <span key={x} className="ss-tag">{x}</span>)}</dd></>
          )}
          <dt>{t('hubs.market.github.audit')}</dt>
          <dd className="font-normal text-ink-2">{t('hubs.market.github.auditHint')}</dd>
        </dl>
      </div>
      <div className="df">
        {url && (
          <a className="ss-btn ghost" href={url} target="_blank" rel="noreferrer">
            <ExternalLink size={14} aria-hidden="true" />
            {t('hubs.market.github.open')}
          </a>
        )}
        <span className="flex-1" />
        {installed ? (
          <span className="ss-st ok">{t('hubs.market.github.installed')}</span>
        ) : (
          <Button variant="primary" onClick={onInstall}>
            <Download size={15} />
            {t('hubs.market.github.install')}
          </Button>
        )}
      </div>
    </DialogShell>
  );
}
