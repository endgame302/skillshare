// Fork-only (endgame302/skillshare): browse MCP servers from the official MCP Registry, GitHub's
// MCP registry and Docker's MCP catalog, and add one to skillshare's MCP config.
import { useState } from 'react';
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query';
import { RefreshCw, Search, ShieldCheck, Star } from 'lucide-react';
import type { MCPMarketEntry, MCPMarketList, MCPMarketQuery, MCPMarketSource } from '../../../../api/forkMcpMarket';
import { forkMcpMarket, mcpMarketKey } from '../../../../api/forkMcpMarket';
import { Checkbox } from '../../../Checkbox';
import EmptyState from '../../../EmptyState';
import SegmentedControl from '../../../SegmentedControl';
import { Select } from '../../../Select';
import Spinner from '../../../Spinner';
import { useToast } from '../../../Toast';
import { formatDateTime, formatNumber, useI18n, useT } from '../../../../i18n';
import MCPMarketInstall from './MCPMarketInstall';
import { kindLabel, mcpMarketErrorText, refusalText } from './labels';
import { useDebounced } from './useDebounced';

const PAGE = 50;

/** Each tab's sort orders, its default first. */
const SORTS: Record<MCPMarketSource, string[]> = {
  official: ['name', 'updated'],
  curated: ['stars', 'name', 'updated'],
  docker: ['pulls', 'stars', 'name'],
};

export default function MCPMarketView() {
  const t = useT();
  const { locale } = useI18n();
  const queryClient = useQueryClient();
  const { toast } = useToast();
  const [source, setSource] = useState<MCPMarketSource>('official');
  const [text, setText] = useState('');
  const q = useDebounced(text.trim(), 300);
  const [kind, setKind] = useState<'' | 'local' | 'remote'>('');
  const [installable, setInstallable] = useState(false);
  const [bulk, setBulk] = useState(false);
  const [deprecated, setDeprecated] = useState(false);
  const [sortChoice, setSortChoice] = useState<string | null>(null);
  const [offset, setOffset] = useState(0);
  const [refreshing, setRefreshing] = useState(false);
  const [open, setOpen] = useState<MCPMarketEntry | null>(null);

  // Relevance is offered, and the default, only while searching.
  const sort = q ? sortChoice ?? '' : sortChoice || SORTS[source][0];
  const official = source === 'official';
  const query: MCPMarketQuery = { q, kind, installable, bulk: official && bulk, deprecated: official && deprecated, sort, offset, limit: PAGE };
  const listKey = [...mcpMarketKey, 'list', source, query] as const;
  const list = useQuery({
    queryKey: listKey,
    queryFn: () => forkMcpMarket.list(source, query),
    placeholderData: keepPreviousData,
    // The official index fills in the background; keep showing what it has so far.
    refetchInterval: (qq) => {
      const st = qq.state.data?.status;
      return st && (st.syncing || st.state === 'loading') ? 3000 : false;
    },
  });
  const data: MCPMarketList | undefined = list.data?.source === source ? list.data : undefined;
  const st = data?.status;

  const reset = () => setOffset(0);
  const changeSource = (s: MCPMarketSource) => {
    setSource(s);
    setSortChoice(null);
    reset();
  };
  const refresh = async () => {
    setRefreshing(true);
    try {
      queryClient.setQueryData(listKey, await forkMcpMarket.list(source, { ...query, refresh: true }));
    } catch (e) {
      toast(mcpMarketErrorText(t, e), 'error');
    } finally {
      setRefreshing(false);
    }
  };
  const date = (iso?: string) => (iso ? formatDateTime(iso, locale, { dateStyle: 'medium', timeStyle: 'short' }) : '');

  const sortOptions = [
    ...(q ? [{ value: '', label: t('mcpmarket.sort.relevance') }] : []),
    ...SORTS[source].map((s) => ({ value: s, label: t(`mcpmarket.sort.${s}`) })),
  ];

  return (
    <div className="@container flex min-w-0 flex-col gap-3">
      <div className="ss-sec !mb-0 flex-wrap !items-center gap-y-2">
        <h2>{t('mcpmarket.title')}</h2>
        {data && <span className="ss-cnt">{formatNumber(data.total, locale)}</span>}
        <SegmentedControl
          className="ml-auto"
          value={source}
          onChange={changeSource}
          options={[
            { value: 'official', label: t('mcpmarket.tab.official'), title: t('mcpmarket.tab.officialHint') },
            { value: 'curated', label: t('mcpmarket.tab.curated'), title: t('mcpmarket.tab.curatedHint') },
            { value: 'docker', label: t('mcpmarket.tab.docker'), title: t('mcpmarket.tab.dockerHint') },
          ]}
        />
      </div>

      <div className="flex min-w-0 flex-wrap items-center gap-2.5">
        <span className="ss-inp sm !h-8 w-[240px] max-w-full">
          <Search size={14} className="shrink-0 text-ink-3" />
          <input value={text} onChange={(e) => { setText(e.target.value); reset(); }} placeholder={t('mcpmarket.filter.search')} aria-label={t('mcpmarket.filter.search')} />
        </span>
        <Select
          size="sm"
          className="w-[170px] shrink-0"
          prefix={t('mcpmarket.filter.kind')}
          value={kind}
          onChange={(v) => { setKind(v as typeof kind); reset(); }}
          options={[
            { value: '', label: t('mcpmarket.filter.allKinds') },
            { value: 'local', label: t('mcpmarket.filter.local') },
            { value: 'remote', label: t('mcpmarket.filter.remote') },
          ]}
        />
        <Select size="sm" className="w-[170px] shrink-0" prefix={t('mcpmarket.filter.sort')} value={sort} onChange={(v) => { setSortChoice(v); reset(); }} options={sortOptions} />
        <Checkbox label={t('mcpmarket.filter.installable')} checked={installable} onChange={(v) => { setInstallable(v); reset(); }} />
        {official && <Checkbox label={t('mcpmarket.filter.bulk')} checked={bulk} onChange={(v) => { setBulk(v); reset(); }} />}
        {official && <Checkbox label={t('mcpmarket.filter.deprecated')} checked={deprecated} onChange={(v) => { setDeprecated(v); reset(); }} />}
        <button
          type="button"
          className="ss-btn ml-auto !w-[34px] !px-0"
          aria-label={t('mcpmarket.refresh')}
          title={t('mcpmarket.refresh')}
          disabled={refreshing}
          onClick={() => void refresh()}
        >
          <RefreshCw size={15} className={refreshing ? 'animate-spin' : ''} />
        </button>
      </div>

      <div className="ss-note warn"><span className="flex-1">{t('mcpmarket.safety')}</span></div>
      {official && st && !st.complete && (
        <div className="ss-note inf">
          {st.syncing && <Spinner size="sm" />}
          <span className="flex-1">{t('mcpmarket.indexing', { count: formatNumber(st.entries, locale) })}</span>
        </div>
      )}
      {st?.error && data && (
        <div className="ss-note warn">
          <span className="flex-1 break-words">
            {t('mcpmarket.refreshFailed', { error: st.error })}
            {st.retryAt && ` ${t('mcpmarket.nextTry', { time: date(st.retryAt) })}`}
          </span>
        </div>
      )}
      {st?.stale && <div className="ss-note warn"><span className="flex-1">{t('mcpmarket.stale')}</span></div>}
      {source === 'docker' && data?.dockerFound === false && (
        <div className="ss-note warn"><span className="flex-1">{t('mcpmarket.dockerMissing')}</span></div>
      )}
      {data && (data.hiddenBulk > 0 || data.hiddenDeprecated > 0) && (
        <p className="m-0 flex flex-wrap items-center gap-x-3 gap-y-1 text-[12.5px] text-ink-2">
          {data.hiddenBulk > 0 && (
            <span>
              {t('mcpmarket.hiddenBulk', { count: formatNumber(data.hiddenBulk, locale), threshold: data.bulkThreshold })}{' '}
              <button type="button" className="ss-more" onClick={() => { setBulk(true); reset(); }}>{t('mcpmarket.showThem')}</button>
            </span>
          )}
          {data.hiddenDeprecated > 0 && <span>{t('mcpmarket.hiddenDeprecated', { count: formatNumber(data.hiddenDeprecated, locale) })}</span>}
        </p>
      )}

      {!data && !list.isError && (
        <div className="ss-box flex items-center gap-2.5 text-[13px] text-ink-2">
          <Spinner size="sm" />
          {t('mcpmarket.loading')}
        </div>
      )}
      {list.isError && !data && (
        <div role="alert" className="ss-note bad">
          <span className="flex-1 break-words">{mcpMarketErrorText(t, list.error)}</span>
          <button type="button" className="ss-btn sm" onClick={() => void list.refetch()}>{t('mcpmarket.retry')}</button>
        </div>
      )}
      {data && data.items.length === 0 && (
        <EmptyState icon={Search} title={st && st.entries === 0 ? t('mcpmarket.empty') : t('mcpmarket.noMatch')} />
      )}

      {data && data.items.length > 0 && (
        <div className="ss-list">
          <div className="ss-lh">
            <span className="flex-1">{t('mcpmarket.col.server')}</span>
            <span className="hidden w-[120px] shrink-0 @xl:block">{t('mcpmarket.col.version')}</span>
            <span className="w-[104px] shrink-0" />
          </div>
          {data.items.map((e) => (
            <Row key={e.name} entry={e} source={source} dockerFound={data.dockerFound} onOpen={() => setOpen(e)} />
          ))}
        </div>
      )}
      {data && data.total > PAGE && (
        <div className="flex items-center justify-center gap-2.5 text-[12.5px] text-ink-2">
          <button type="button" className="ss-btn sm" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - PAGE))}>{t('mcpmarket.prev')}</button>
          <span>{t('mcpmarket.range', { from: formatNumber(offset + 1, locale), to: formatNumber(Math.min(offset + PAGE, data.total), locale), total: formatNumber(data.total, locale) })}</span>
          <button type="button" className="ss-btn sm" disabled={offset + PAGE >= data.total} onClick={() => setOffset(offset + PAGE)}>{t('mcpmarket.next')}</button>
        </div>
      )}
      {st?.fetchedAt && <span className="text-[12px] text-ink-3">{t(`mcpmarket.source.${source}`, { date: date(st.fetchedAt) })}</span>}

      {open && <MCPMarketInstall source={source} entry={open} onClose={() => setOpen(null)} />}
    </div>
  );
}

function Row({ entry: e, source, dockerFound, onOpen }: { entry: MCPMarketEntry; source: MCPMarketSource; dockerFound?: boolean; onOpen: () => void }) {
  const t = useT();
  const { locale } = useI18n();
  const kinds = [...new Set((e.kinds ?? []).map((k) => kindLabel(t, k)).filter(Boolean))];
  const container = source === 'docker' && dockerFound === false && e.local;
  return (
    <div className="ss-r flex-wrap @md:flex-nowrap">
      <span className="flex min-w-0 flex-1 basis-full flex-col gap-0.5 @md:basis-0">
        <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
          <button type="button" className="nm max-w-full truncate text-start hover:underline" onClick={onOpen}>{e.title || e.name}</button>
          {kinds.map((k) => <span key={k} className="ss-tag shrink-0">{k}</span>)}
          {e.status === 'deprecated' && <span className="ss-tag warn shrink-0">{t('mcpmarket.deprecated')}</span>}
          {e.dockerBuilt && <span className="ss-tag ok shrink-0" title={t('mcpmarket.dockerBuiltHint')}><ShieldCheck size={11} aria-hidden="true" />{t('mcpmarket.dockerBuilt')}</span>}
          {e.official && <span className="ss-tag ok shrink-0">{t('mcpmarket.inOfficial')}</span>}
          {container && <span className="ss-tag warn shrink-0">{t('mcpmarket.dockerNotFound')}</span>}
          {!!e.stars && <span className="ss-tag shrink-0"><Star size={11} aria-hidden="true" />{formatNumber(e.stars, locale)}</span>}
          {!!e.pulls && <span className="ss-tag shrink-0">{t('mcpmarket.pulls', { count: formatNumber(e.pulls, locale) })}</span>}
          {e.license && <span className="ss-tag shrink-0">{e.license}</span>}
          {e.category && <span className="ss-tag shrink-0">{e.category}</span>}
        </span>
        <span className="truncate font-mono text-[11.5px] text-ink-3">
          {e.name}
          {e.version && <span className="@xl:hidden"> · {e.version}</span>}
        </span>
        {e.description && <span className="truncate text-[13px] text-ink-2">{e.description}</span>}
      </span>
      <span className="hidden w-[120px] shrink-0 truncate font-mono text-[12px] text-ink-2 @xl:block">{e.version}</span>
      <span className="ml-auto flex w-[104px] shrink-0 justify-end">
        {e.installable ? (
          <button type="button" className="ss-btn sm" onClick={onOpen}>{t('mcpmarket.add')}</button>
        ) : (
          <span className="ss-st off" title={e.blocked ? refusalText(t, e.blocked) : undefined}>{t('mcpmarket.notSupported')}</span>
        )}
      </span>
    </div>
  );
}
