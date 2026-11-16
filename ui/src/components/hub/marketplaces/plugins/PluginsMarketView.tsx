// Fork-only (endgame302/skillshare): browse Claude Code plugin catalogs and install a plugin from one.
import { useMemo, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { ExternalLink, RefreshCw, Search } from 'lucide-react';
import type { CatalogEntry, PluginCatalogId } from '../../../../api/forkPlugins';
import { forkPlugins, pluginCatalogKey } from '../../../../api/forkPlugins';
import { pluginsApi } from '../../../../api/plugins';
import EmptyState from '../../../EmptyState';
import { Select } from '../../../Select';
import Spinner from '../../../Spinner';
import { useToast } from '../../../Toast';
import { useAppContext } from '../../../../context/AppContext';
import { formatDateTime, useI18n, useT } from '../../../../i18n';
import { queryKeys } from '../../../../lib/queryKeys';
import { catalogErrorText, isInstalled, matchesText, safeHomepage } from './helpers';
import PluginAddFlow from './PluginAddFlow';
import PluginHandoffDialog from './PluginHandoffDialog';

type InstallFilter = '' | 'add' | 'native';

/** Rows drawn at first and per "show more". */
const PAGE = 100;

export default function PluginsMarketView() {
  const t = useT();
  const { locale } = useI18n();
  const { isProjectMode } = useAppContext();
  const queryClient = useQueryClient();
  const { toast } = useToast();
  const [catalogId, setCatalogId] = useState<PluginCatalogId>('official');
  const [category, setCategory] = useState('');
  const [installFilter, setInstallFilter] = useState<InstallFilter>('');
  const [query, setQuery] = useState('');
  const [limit, setLimit] = useState(PAGE);
  const [refreshing, setRefreshing] = useState(false);
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [open, setOpen] = useState<CatalogEntry | null>(null);

  const catalog = useQuery({
    queryKey: pluginCatalogKey(catalogId),
    queryFn: () => forkPlugins.catalog(catalogId),
    staleTime: 5 * 60_000,
  });
  // Config's answer, without asking every Agent's CLI: enough to know what skillshare manages.
  const inventory = useQuery({ queryKey: queryKeys.pluginPackages, queryFn: () => pluginsApi.list(false) });

  const entries = useMemo(() => catalog.data?.entries ?? [], [catalog.data]);
  const categories = useMemo(
    () => [...new Set(entries.map((e) => e.category ?? '').filter(Boolean))].sort((a, b) => a.localeCompare(b)),
    [entries],
  );
  const shown = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return entries.filter((e) =>
      (!category || e.category === category) && (!installFilter || e.install === installFilter) && matchesText(e, needle));
  }, [entries, category, installFilter, query]);

  const changeCatalog = (next: string) => {
    setCatalogId(next as PluginCatalogId);
    setCategory('');
    setLimit(PAGE);
  };

  const refresh = async () => {
    const id = catalogId;
    setRefreshing(true);
    try {
      queryClient.setQueryData(pluginCatalogKey(id), await forkPlugins.catalog(id, true));
    } catch (e) {
      toast(catalogErrorText(t, e), 'error');
    } finally {
      setRefreshing(false);
    }
  };

  const date = (iso?: string) => (iso ? formatDateTime(iso, locale, { dateStyle: 'medium', timeStyle: 'short' }) : '');
  const market = catalog.data?.market ?? '';

  const action = (e: CatalogEntry) => {
    if (isInstalled(inventory.data, e, market)) return <span className="ss-st ok">{t('hubs.browse.installed')}</span>;
    switch (e.install) {
      case 'add':
        return <button type="button" className="ss-btn sm" onClick={() => setOpen(e)}>{t('hubs.browse.install')}</button>;
      case 'native':
        return isProjectMode
          ? <span className="ss-st off" title={t('pluginsMarket.handoff.error.projectMode')}>{t('pluginsMarket.globalOnly')}</span>
          : <button type="button" className="ss-btn sm" onClick={() => setOpen(e)}>{t('hubs.browse.install')}</button>;
      case 'manual':
        return <span className="text-[12px] text-ink-2" title={`/plugin install ${e.name}@${market}`}>{t('pluginsMarket.manual')}</span>;
      default:
        return <span className="ss-st off" title={e.problem}>{t('pluginsMarket.notInstallable')}</span>;
    }
  };

  return (
    // A container: the pane is narrow beside the hub list, so rows adapt to it, not to the window.
    <div className="@container flex min-w-0 flex-col gap-3">
      <div className="ss-sec !mb-0 flex-wrap !items-center gap-y-2">
        <h2>{t('pluginsMarket.title')}</h2>
        {catalog.data && <span className="ss-cnt">{entries.length}</span>}
        <span className="ml-auto flex min-w-0 flex-wrap items-center justify-end gap-2.5">
          <Select
            size="sm"
            className="w-[230px] shrink-0"
            ariaLabel={t('pluginsMarket.filter.catalog')}
            prefix={t('pluginsMarket.filter.catalog')}
            value={catalogId}
            onChange={changeCatalog}
            options={[
              { value: 'official', label: t('pluginsMarket.catalog.official') },
              { value: 'community', label: t('pluginsMarket.catalog.community') },
              { value: 'wshobson', label: t('pluginsMarket.catalog.wshobson') },
            ]}
          />
          {categories.length > 0 && (
            <Select
              size="sm"
              className="w-[180px] shrink-0"
              ariaLabel={t('pluginsMarket.filter.category')}
              prefix={t('pluginsMarket.filter.category')}
              value={category}
              onChange={(v) => { setCategory(v); setLimit(PAGE); }}
              options={[{ value: '', label: t('pluginsMarket.filter.all') }, ...categories.map((c) => ({ value: c, label: c }))]}
            />
          )}
          <Select
            size="sm"
            className="w-[210px] shrink-0"
            ariaLabel={t('pluginsMarket.filter.install')}
            prefix={t('pluginsMarket.filter.install')}
            value={installFilter}
            onChange={(v) => { setInstallFilter(v as InstallFilter); setLimit(PAGE); }}
            options={[
              { value: '', label: t('pluginsMarket.filter.all') },
              { value: 'add', label: t('pluginsMarket.filter.add') },
              { value: 'native', label: t('pluginsMarket.filter.native') },
            ]}
          />
          <span className="ss-inp sm !h-8 w-[220px] max-w-full">
            <Search size={14} className="shrink-0 text-ink-3" />
            <input
              value={query}
              onChange={(e) => { setQuery(e.target.value); setLimit(PAGE); }}
              placeholder={t('pluginsMarket.filter.search')}
              aria-label={t('pluginsMarket.filter.search')}
            />
          </span>
          <button
            type="button"
            className="ss-btn !w-[34px] !px-0"
            aria-label={t('pluginsMarket.refresh')}
            title={t('pluginsMarket.refresh')}
            disabled={refreshing || catalog.isFetching}
            onClick={() => void refresh()}
          >
            <RefreshCw size={15} className={refreshing ? 'animate-spin' : ''} />
          </button>
        </span>
      </div>

      <div className="ss-note warn"><span className="flex-1">{t('pluginsMarket.thirdParty')}</span></div>
      {catalog.data && (
        <div className="ss-note">
          <span className="flex-1 break-words">
            {t('pluginsMarket.catalogNote', { repo: catalog.data.repo, commit: catalog.data.commit.slice(0, 7), market })}
          </span>
        </div>
      )}
      {catalog.data?.stale && (
        <div className="ss-note warn">
          <span className="flex-1 break-words">
            {t('pluginsMarket.stale', { date: date(catalog.data.fetchedAt), reason: catalog.data.staleReason ?? '' })}
          </span>
        </div>
      )}

      {catalog.isPending && (
        <div className="ss-box flex items-center gap-2.5 text-[13px] text-ink-2">
          <Spinner size="sm" />
          {t('pluginsMarket.loading')}
        </div>
      )}
      {catalog.isError && (
        <div role="alert" className="ss-note bad">
          <span className="flex-1 break-words">{catalogErrorText(t, catalog.error)}</span>
          <button type="button" className="ss-btn sm" onClick={() => void catalog.refetch()}>{t('pluginsMarket.retry')}</button>
        </div>
      )}
      {catalog.data && shown.length === 0 && (
        <EmptyState icon={Search} title={entries.length === 0 ? t('pluginsMarket.empty') : t('pluginsMarket.noMatch')} />
      )}

      {shown.length > 0 && (
        <div className="ss-list">
          <div className="ss-lh">
            <span className="flex-1">{t('pluginsMarket.col.plugin')}</span>
            <span className="w-[120px] shrink-0" />
          </div>
          {shown.slice(0, limit).map((e) => {
            const isExpanded = Boolean(expanded[e.name]);
            const homepage = safeHomepage(e.homepage);
            return (
              <div key={e.name} className="ss-r flex-wrap @md:flex-nowrap">
                <span className="flex min-w-0 flex-1 basis-full flex-col gap-0.5 @md:basis-0">
                  <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
                    <button
                      type="button"
                      className="nm max-w-full truncate text-start hover:underline"
                      aria-expanded={isExpanded}
                      onClick={() => setExpanded((m) => ({ ...m, [e.name]: !m[e.name] }))}
                    >
                      {e.displayName || e.name}
                    </button>
                    {e.category && <span className="ss-tag shrink-0">{e.category}</span>}
                    {e.install === 'add' && <span className="ss-tag inf shrink-0" title={t('pluginsMarket.tag.inCatalogHint')}>{t('pluginsMarket.tag.inCatalog')}</span>}
                    {e.install === 'native' && <span className="ss-tag shrink-0" title={t('pluginsMarket.tag.externalHint')}>{e.source.kind}</span>}
                    {e.install === 'manual' && <span className="ss-tag shrink-0">{t('pluginsMarket.tag.command')}</span>}
                    {e.pinned && <span className="ss-tag ok shrink-0">{t('pluginsMarket.tag.pinned')}</span>}
                    {e.install === 'native' && !e.pinned && <span className="ss-tag warn shrink-0" title={t('pluginsMarket.handoff.unpinned')}>{t('pluginsMarket.tag.notPinned')}</span>}
                  </span>
                  <span className="truncate font-mono text-[11.5px] text-ink-3">
                    {e.name}@{market}
                    {e.author && ` · ${e.author}`}
                    {e.version && ` · ${e.version}`}
                  </span>
                  {e.description && (
                    <span className={isExpanded ? 'whitespace-pre-wrap break-words text-[13px] text-ink-2' : 'truncate text-[13px] text-ink-2'}>
                      {e.description}
                    </span>
                  )}
                </span>
                <span className="ml-auto flex shrink-0 items-center justify-end gap-2">
                  {homepage && (
                    <a
                      href={homepage}
                      target="_blank"
                      rel="noreferrer"
                      className="ss-btn sm !w-[30px] !px-0"
                      aria-label={t('pluginsMarket.openHomepage')}
                      title={t('pluginsMarket.openHomepage')}
                    >
                      <ExternalLink size={13} aria-hidden="true" />
                    </a>
                  )}
                  {action(e)}
                </span>
              </div>
            );
          })}
        </div>
      )}
      {shown.length > limit && (
        <button type="button" className="ss-btn self-center" onClick={() => setLimit((n) => n + PAGE)}>
          {t('pluginsMarket.showMore', { count: Math.min(PAGE, shown.length - limit), total: shown.length })}
        </button>
      )}

      {open && catalog.data && open.install === 'add' && (
        <PluginAddFlow catalog={catalog.data} entry={open} onClose={() => setOpen(null)} />
      )}
      {open && catalog.data && open.install === 'native' && (
        <PluginHandoffDialog catalog={catalog.data} entry={open} onClose={() => setOpen(null)} />
      )}
    </div>
  );
}
