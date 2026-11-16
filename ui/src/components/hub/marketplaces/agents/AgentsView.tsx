// Fork-only (endgame302/skillshare): browse third-party agent directories and install an agent.
import { useMemo, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { ExternalLink, RefreshCw, Search } from 'lucide-react';
import { Link } from 'react-router-dom';
import { api, ApiError } from '../../../../api/client';
import type { AgentSource, MarketAgent } from '../../../../api/forkAgents';
import { agentInstallSource, agentListingKey, forkAgents } from '../../../../api/forkAgents';
import EmptyState from '../../../EmptyState';
import { Select } from '../../../Select';
import Spinner from '../../../Spinner';
import { useToast } from '../../../Toast';
import { useSkillsQuery, useSyncedTargetsQuery } from '../../../../hooks/useSharedQueries';
import { formatDateTime, formatNumber, useI18n, useT } from '../../../../i18n';
import { invalidate } from '../../../../lib/queryEvents';
import { agentTargets, codexTargetName } from './agentTargets';

const PAGE = 100;

const SCAN_TONE: Record<string, string> = {
  LOW: 'ok',
  MEDIUM: 'warn',
  HIGH: 'bad',
  CRITICAL: 'bad',
};

function agentErrorText(t: ReturnType<typeof useT>, err: unknown): string {
  if (err instanceof ApiError && err.code === 'agents.rate_limited') {
    return t('agentsMarket.error.rateLimited');
  }
  const message = err instanceof Error ? err.message : String(err);
  return t('agentsMarket.error.load', { message });
}

export default function AgentsView() {
  const t = useT();
  const { locale } = useI18n();
  const queryClient = useQueryClient();
  const { toast } = useToast();

  const [source, setSource] = useState<AgentSource>('voltagent');
  const [category, setCategory] = useState('');
  const [query, setQuery] = useState('');
  const [limit, setLimit] = useState(PAGE);
  const [refreshing, setRefreshing] = useState(false);
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [installing, setInstalling] = useState<string | null>(null);
  const [errors, setErrors] = useState<Record<string, string>>({});

  const listing = useQuery({
    queryKey: agentListingKey(source),
    queryFn: () => forkAgents.listing(source),
    staleTime: 5 * 60_000,
  });
  const { data: skillsData } = useSkillsQuery();
  const { data: targetsData } = useSyncedTargetsQuery();

  const agents = useMemo(() => listing.data?.agents ?? [], [listing.data]);

  const categories = useMemo(
    () => Array.from(new Set(agents.map((a) => a.category).filter(Boolean))).sort((a, b) => a.localeCompare(b)),
    [agents],
  );

  const shown = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return agents.filter((a) => {
      if (category && a.category !== category) return false;
      if (!needle) return true;
      return (
        a.title.toLowerCase().includes(needle) ||
        a.name.toLowerCase().includes(needle) ||
        a.description.toLowerCase().includes(needle) ||
        a.category.toLowerCase().includes(needle)
      );
    });
  }, [agents, category, query]);

  const installedNames = useMemo(() => {
    const names = new Set<string>();
    for (const r of skillsData?.resources ?? []) {
      if (r.kind !== 'agent') continue;
      names.add(r.name);
      const slash = r.name.lastIndexOf('/');
      if (slash >= 0) names.add(r.name.slice(slash + 1));
    }
    return names;
  }, [skillsData]);

  const targets = useMemo(() => targetsData?.targets ?? [], [targetsData]);
  const targetSummary = useMemo(() => agentTargets(targets), [targets]);
  const codexName = useMemo(() => codexTargetName(targets), [targets]);

  const changeSource = (next: string) => {
    setSource(next as AgentSource);
    setCategory('');
    setLimit(PAGE);
  };

  const refresh = async () => {
    const activeSource = source;
    setRefreshing(true);
    try {
      queryClient.setQueryData(agentListingKey(activeSource), await forkAgents.listing(activeSource, true));
    } catch (e) {
      toast(agentErrorText(t, e), 'error');
    } finally {
      setRefreshing(false);
    }
  };

  const install = async (a: MarketAgent) => {
    setInstalling(a.id);
    setErrors((m) => {
      const next = { ...m };
      delete next[a.id];
      return next;
    });
    try {
      const res = await api.installBatch({
        source: agentInstallSource(a),
        skills: [{ name: a.name, path: a.file }],
        kind: 'agent',
      });
      const item = res.results[0];
      if (item?.error) {
        setErrors((m) => ({ ...m, [a.id]: item.error! }));
        toast(item.error, 'error');
      } else if (item?.action === 'skipped') {
        toast(t('agentsMarket.toast.skipped', { name: a.name }), 'warning');
      } else {
        toast(t('agentsMarket.toast.installed', { name: a.name }), 'success');
        for (const w of item?.warnings ?? []) toast(w, 'warning');
        if (res.summary.toLowerCase().includes('none of your configured targets support agents')) {
          toast(t('agentsMarket.toast.noTargets'), 'warning');
        }
      }
      void invalidate(queryClient, 'skillsChanged');
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      setErrors((m) => ({ ...m, [a.id]: msg }));
      toast(msg, 'error');
    } finally {
      setInstalling(null);
    }
  };

  const date = (iso?: string) => (iso ? formatDateTime(iso, locale, { dateStyle: 'medium', timeStyle: 'short' }) : '');
  const commit = (listing.data?.commit ?? '').slice(0, 7);

  return (
    <div className="@container flex min-w-0 flex-col gap-3">
      <div className="ss-sec !mb-0 flex-wrap !items-center gap-y-2">
        <h2>{t('agentsMarket.title')}</h2>
        {listing.data && <span className="ss-cnt">{agents.length}</span>}
        <span className="ml-auto flex min-w-0 flex-wrap items-center justify-end gap-2.5">
          <Select
            size="sm"
            className="w-[225px] shrink-0"
            ariaLabel={t('agentsMarket.filter.source')}
            prefix={t('agentsMarket.filter.source')}
            value={source}
            onChange={changeSource}
            options={[
              { value: 'voltagent', label: t('agentsMarket.source.voltagent') },
              { value: 'wshobson', label: t('agentsMarket.source.wshobson') },
              { value: 'aitmpl', label: t('agentsMarket.source.aitmpl') },
            ]}
          />
          <Select
            size="sm"
            className="w-[180px] shrink-0"
            ariaLabel={t('agentsMarket.filter.category')}
            prefix={t('agentsMarket.filter.category')}
            value={category}
            onChange={(v) => {
              setCategory(v);
              setLimit(PAGE);
            }}
            options={[
              { value: '', label: t('agentsMarket.filter.allCategories') },
              ...categories.map((c) => ({ value: c, label: c })),
            ]}
          />
          <span className="ss-inp sm !h-8 w-[220px] max-w-full">
            <Search size={14} className="shrink-0 text-ink-3" />
            <input
              value={query}
              onChange={(e) => {
                setQuery(e.target.value);
                setLimit(PAGE);
              }}
              placeholder={t('agentsMarket.filter.search')}
              aria-label={t('agentsMarket.filter.search')}
            />
          </span>
          <button
            type="button"
            className="ss-btn !w-[34px] !px-0"
            aria-label={t('agentsMarket.refresh')}
            title={t('agentsMarket.refresh')}
            disabled={refreshing || listing.isFetching}
            onClick={() => void refresh()}
          >
            <RefreshCw size={15} className={refreshing ? 'animate-spin' : ''} />
          </button>
        </span>
      </div>

      <div className="ss-note warn">
        <span className="flex-1">{t('agentsMarket.thirdParty')}</span>
      </div>

      {listing.data && source === 'voltagent' && (
        <div className="ss-note">
          <span className="flex-1">
            {t('agentsMarket.sourceNote.voltagentPrefix', { commit })}{' '}
            <a
              href="https://github.com/VoltAgent/awesome-codex-subagents"
              target="_blank"
              rel="noreferrer"
              className="underline hover:text-ink"
            >
              {t('agentsMarket.sourceNote.voltagentLink')}
            </a>{' '}
            {t('agentsMarket.sourceNote.voltagentSuffix')}
          </span>
        </div>
      )}
      {listing.data && source === 'wshobson' && (
        <div className="ss-note">
          <span className="flex-1">{t('agentsMarket.sourceNote.wshobson', { commit })}</span>
        </div>
      )}
      {listing.data && source === 'aitmpl' && (
        <div className="ss-note">
          <span className="flex-1">{t('agentsMarket.sourceNote.aitmpl', { date: date(listing.data.generatedAt) })}</span>
        </div>
      )}

      {listing.data?.stale && (
        <div className="ss-note warn">
          <span className="flex-1 break-words">
            {t('agentsMarket.stale', { date: date(listing.data.fetchedAt), reason: listing.data.staleReason ?? '' })}
          </span>
        </div>
      )}

      {targetsData && (
        <div className="ss-box flex flex-col gap-2">
          <h3 className="text-[13px] font-semibold text-ink">{t('agentsMarket.targets.heading')}</h3>
          {targetSummary.receiving.length > 0 ? (
            <ul className="flex flex-col gap-1 text-[13px]">
              {targetSummary.receiving.map((rt) => (
                <li key={rt.name} className="flex flex-wrap items-baseline gap-1.5">
                  <span className="font-medium text-ink">{rt.name}</span>
                  <span aria-hidden="true" className="text-ink-3">→</span>
                  <span className="break-all font-mono text-[12px] text-ink-2">{rt.path}</span>
                </li>
              ))}
            </ul>
          ) : (
            <div className="ss-note warn">
              <span className="flex-1">{t('agentsMarket.targets.none')}</span>
            </div>
          )}
          {(targetSummary.codex === 'missing' || targetSummary.codex === 'noExtension') && codexName && (
            <div className="ss-note inf">
              <span className="flex-1">
                {t('agentsMarket.targets.codex')}{' '}
                <Link to={`/targets/${encodeURIComponent(codexName)}?tab=agents`} className="underline hover:text-ink">
                  {t('agentsMarket.targets.codexLink')}
                </Link>
              </span>
            </div>
          )}
          {targetSummary.gemini && (
            <div className="ss-note">
              <span className="flex-1">{t('agentsMarket.targets.gemini')}</span>
            </div>
          )}
        </div>
      )}

      {listing.isPending && (
        <div className="ss-box flex items-center gap-2.5 text-[13px] text-ink-2">
          <Spinner size="sm" />
          {t('agentsMarket.loading')}
        </div>
      )}
      {listing.isError && (
        <div role="alert" className="ss-note bad">
          <span className="flex-1 break-words">{agentErrorText(t, listing.error)}</span>
          <button type="button" className="ss-btn sm" onClick={() => void listing.refetch()}>
            {t('agentsMarket.retry')}
          </button>
        </div>
      )}
      {listing.data && shown.length === 0 && (
        <EmptyState icon={Search} title={agents.length === 0 ? t('agentsMarket.empty') : t('agentsMarket.noMatch')} />
      )}

      {shown.length > 0 && (
        <div className="ss-list">
          <div className="ss-lh">
            <span className="flex-1">{t('agentsMarket.col.agent')}</span>
            <span className="w-[104px] shrink-0" />
          </div>
          {shown.slice(0, limit).map((a) => {
            const isExpanded = Boolean(expanded[a.id]);
            const isInstalled = installedNames.has(a.name);
            const scanTone = a.scan ? SCAN_TONE[a.scan.severity.toUpperCase()] ?? '' : '';
            return (
              <div key={a.id} className="ss-r flex-wrap @md:flex-nowrap">
                <span className="flex min-w-0 flex-1 basis-full flex-col gap-0.5 @md:basis-0">
                  <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
                    <button
                      type="button"
                      className="nm max-w-full truncate text-start hover:underline"
                      aria-expanded={isExpanded}
                      onClick={() => setExpanded((m) => ({ ...m, [a.id]: !m[a.id] }))}
                    >
                      {a.title || a.name}
                    </button>
                    {a.category && <span className="ss-tag shrink-0">{a.category}</span>}
                    {typeof a.downloads === 'number' && (
                      <span className="ss-tag shrink-0">
                        {t('agentsMarket.downloads', { count: formatNumber(a.downloads, locale) })}
                      </span>
                    )}
                    {a.scan && (
                      <span
                        className={`ss-tag ${scanTone} shrink-0`.trim()}
                        title={t('agentsMarket.scanHint')}
                      >
                        {a.scan.score !== undefined
                          ? t('agentsMarket.scanWithScore', { severity: a.scan.severity, score: a.scan.score })
                          : t('agentsMarket.scan', { severity: a.scan.severity })}
                      </span>
                    )}
                  </span>
                  <span className="truncate font-mono text-[11.5px] text-ink-3">
                    {a.name}.md
                    {a.tools && a.tools.length > 0 && ` · ${a.tools.join(', ')}`}
                    {a.model && ` · ${a.model}`}
                  </span>
                  {a.description && (
                    <span className={isExpanded ? 'whitespace-pre-wrap break-words text-[13px] text-ink-2' : 'truncate text-[13px] text-ink-2'}>
                      {a.description}
                    </span>
                  )}
                  {errors[a.id] && <span role="alert" className="break-words text-[12px] text-bad">{errors[a.id]}</span>}
                </span>
                <span className="ml-auto flex shrink-0 items-center justify-end gap-2">
                  {a.pageUrl && (
                    <a
                      href={a.pageUrl}
                      target="_blank"
                      rel="noreferrer"
                      className="ss-btn sm !w-[30px] !px-0"
                      aria-label={t('agentsMarket.openPage')}
                      title={t('agentsMarket.openPage')}
                    >
                      <ExternalLink size={13} aria-hidden="true" />
                    </a>
                  )}
                  {isInstalled ? (
                    <span className="ss-st ok">{t('hubs.browse.installed')}</span>
                  ) : (
                    <button
                      type="button"
                      className="ss-btn sm"
                      disabled={installing !== null}
                      onClick={() => void install(a)}
                    >
                      {installing === a.id ? <Spinner size="sm" /> : t('hubs.browse.install')}
                    </button>
                  )}
                </span>
              </div>
            );
          })}
        </div>
      )}

      {shown.length > limit && (
        <button type="button" className="ss-btn self-center" onClick={() => setLimit((n) => n + PAGE)}>
          {t('agentsMarket.showMore', { count: Math.min(PAGE, shown.length - limit), total: shown.length })}
        </button>
      )}
    </div>
  );
}
