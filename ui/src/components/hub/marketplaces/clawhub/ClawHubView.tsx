// Fork-only (endgame302/skillshare): browse ClawHub's skills feed and install a hosted skill.
import { useMemo, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Github, RefreshCw, Search, ShieldCheck, Star } from 'lucide-react';
import { ApiError } from '../../../../api/client';
import type { ClawHubItem } from '../../../../api/forkClawhub';
import { clawhubFeedKey, forkClawhub } from '../../../../api/forkClawhub';
import { Checkbox } from '../../../Checkbox';
import ConfirmDialog from '../../../ConfirmDialog';
import EmptyState from '../../../EmptyState';
import { Select } from '../../../Select';
import Spinner from '../../../Spinner';
import { useToast } from '../../../Toast';
import { useSkillsQuery } from '../../../../hooks/useSharedQueries';
import { formatDateTime, useI18n, useT } from '../../../../i18n';
import { invalidate } from '../../../../lib/queryEvents';
import ClawHubDetails from './ClawHubDetails';
import { emptyFilter, filterClawHub, installedVersions, trustLevels } from './filter';
import type { ClawHubFilter } from './filter';
import { clawhubErrorText, trustLabel } from './labels';

/** Rows drawn at first and per "show more": the feed runs to a thousand entries. */
const PAGE = 100;

export default function ClawHubView() {
  const t = useT();
  const { locale } = useI18n();
  const queryClient = useQueryClient();
  const { toast } = useToast();
  const feed = useQuery({ queryKey: clawhubFeedKey, queryFn: () => forkClawhub.feed(), staleTime: 5 * 60_000 });
  const [refreshing, setRefreshing] = useState(false);
  const [filter, setFilter] = useState<ClawHubFilter>(emptyFilter);
  const [limit, setLimit] = useState(PAGE);
  const [openID, setOpenID] = useState<string | null>(null);
  const [installing, setInstalling] = useState<string | null>(null);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [replace, setReplace] = useState<ClawHubItem | null>(null);

  const items = useMemo(() => feed.data?.items ?? [], [feed.data]);
  const shown = useMemo(() => filterClawHub(items, filter), [items, filter]);
  const trusts = useMemo(() => trustLevels(items), [items]);
  const { data: skillsData } = useSkillsQuery();
  const installed = useMemo(() => installedVersions(skillsData?.resources ?? [], items), [skillsData, items]);
  const open = openID ? items.find((it) => it.id === openID) : undefined;

  const update = (patch: Partial<ClawHubFilter>) => {
    setFilter((f) => ({ ...f, ...patch }));
    setLimit(PAGE);
  };

  const refresh = async () => {
    setRefreshing(true);
    try {
      queryClient.setQueryData(clawhubFeedKey, await forkClawhub.feed(true));
    } catch (e) {
      toast(clawhubErrorText(t, e), 'error');
    } finally {
      setRefreshing(false);
    }
  };

  const install = async (item: ClawHubItem, force = false) => {
    setInstalling(item.id);
    setErrors((m) => {
      const next = { ...m };
      delete next[item.id];
      return next;
    });
    try {
      const res = await forkClawhub.install(item.id, item.version, force);
      toast(t('clawhub.toast.installed', { name: res.skillName, version: res.version }), 'success');
      for (const w of res.warnings ?? []) toast(w, 'warning');
      void invalidate(queryClient, 'skillsChanged');
    } catch (e) {
      // The name is taken: replacing a skill is confirmed first.
      if (!force && e instanceof ApiError && e.code === 'clawhub_exists') {
        setOpenID(null);
        setReplace(item);
      } else {
        const text = clawhubErrorText(t, e);
        setErrors((m) => ({ ...m, [item.id]: text }));
        toast(text, 'error');
      }
    } finally {
      setInstalling(null);
    }
  };

  const date = (iso?: string) => (iso ? formatDateTime(iso, locale, { dateStyle: 'medium', timeStyle: 'short' }) : '');

  return (
    // A container: the pane is narrow beside the hub list, so rows adapt to it, not to the window.
    <div className="@container flex min-w-0 flex-col gap-3">
      <div className="ss-sec !mb-0 flex-wrap !items-center gap-y-2">
        <h2>{t('clawhub.title')}</h2>
        {feed.data && <span className="ss-cnt">{items.length}</span>}
        <span className="ml-auto flex min-w-0 flex-wrap items-center justify-end gap-2.5">
          <Select
            size="sm"
            className="w-[170px] shrink-0"
            prefix={t('clawhub.filter.source')}
            value={filter.source}
            onChange={(v) => update({ source: v as ClawHubFilter['source'] })}
            options={[
              { value: 'all', label: t('clawhub.filter.allSources') },
              { value: 'clawhub', label: t('clawhub.filter.hosted') },
              { value: 'github', label: t('clawhub.filter.github') },
            ]}
          />
          {trusts.length > 1 && (
            <Select
              size="sm"
              className="w-[150px] shrink-0"
              prefix={t('clawhub.filter.trust')}
              value={filter.trust}
              onChange={(v) => update({ trust: v })}
              options={[{ value: '', label: t('clawhub.filter.anyTrust') }, ...trusts.map((v) => ({ value: v, label: trustLabel(t, v) }))]}
            />
          )}
          <Checkbox label={t('clawhub.filter.featured')} checked={filter.featuredOnly} onChange={(v) => update({ featuredOnly: v })} />
          <span className="ss-inp sm !h-8 w-[220px] max-w-full">
            <Search size={14} className="shrink-0 text-ink-3" />
            <input value={filter.text} onChange={(e) => update({ text: e.target.value })} placeholder={t('clawhub.filter.search')} aria-label={t('clawhub.filter.search')} />
          </span>
          <button
            type="button"
            className="ss-btn !w-[34px] !px-0"
            aria-label={t('clawhub.refresh')}
            title={t('clawhub.refresh')}
            disabled={refreshing || feed.isFetching}
            onClick={() => void refresh()}
          >
            <RefreshCw size={15} className={refreshing ? 'animate-spin' : ''} />
          </button>
        </span>
      </div>

      <div className="ss-note warn"><span className="flex-1">{t('clawhub.thirdParty')}</span></div>
      {feed.data?.stale && (
        <div className="ss-note warn">
          <span className="flex-1 break-words">{t('clawhub.stale', { date: date(feed.data.fetchedAt), reason: feed.data.staleReason ?? '' })}</span>
        </div>
      )}

      {feed.isPending && (
        <div className="ss-box flex items-center gap-2.5 text-[13px] text-ink-2">
          <Spinner size="sm" />
          {t('clawhub.loading')}
        </div>
      )}
      {feed.isError && (
        <div role="alert" className="ss-note bad">
          <span className="flex-1 break-words">{clawhubErrorText(t, feed.error)}</span>
          <button type="button" className="ss-btn sm" onClick={() => void feed.refetch()}>{t('clawhub.retry')}</button>
        </div>
      )}
      {feed.data && shown.length === 0 && (
        <EmptyState icon={Search} title={items.length === 0 ? t('clawhub.empty') : t('clawhub.noMatch')} />
      )}

      {shown.length > 0 && (
        <div className="ss-list">
          <div className="ss-lh">
            <span className="flex-1">{t('clawhub.col.skill')}</span>
            <span className="hidden w-[110px] shrink-0 @xl:block">{t('clawhub.col.version')}</span>
            <span className="w-[104px] shrink-0" />
          </div>
          {shown.slice(0, limit).map((it) => {
            const have = installed.get(it.id);
            const version = it.source === 'github' && it.github && !it.installable ? it.github.commit.slice(0, 7) : it.version;
            return (
              <div key={it.id} className="ss-r flex-wrap @md:flex-nowrap">
                <span className="flex min-w-0 flex-1 basis-full flex-col gap-0.5 @md:basis-0">
                  <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
                    <button type="button" className="nm max-w-full truncate text-start hover:underline" onClick={() => setOpenID(it.id)}>
                      {it.title}
                    </button>
                    {it.featured && <span className="ss-tag inf shrink-0"><Star size={11} aria-hidden="true" />{t('clawhub.featured')}</span>}
                    {it.trust && <span className="ss-tag ok shrink-0" title={t('clawhub.trust.hint')}><ShieldCheck size={11} aria-hidden="true" />{trustLabel(t, it.trust)}</span>}
                  </span>
                  <span className="truncate font-mono text-[11.5px] text-ink-3">
                    {it.id}
                    <span className="@xl:hidden"> · {version}</span>
                  </span>
                  {it.description && <span className="truncate text-[13px] text-ink-2">{it.description}</span>}
                  {errors[it.id] && <span role="alert" className="truncate text-[12px] text-bad">{errors[it.id]}</span>}
                </span>
                <span className="hidden w-[110px] shrink-0 truncate font-mono text-[12px] text-ink-2 @xl:block">{version}</span>
                <span className="ml-auto flex w-[104px] shrink-0 justify-end">
                  {!it.installable ? (
                    it.blocked === 'github' ? (
                      <button type="button" className="ss-tag" onClick={() => setOpenID(it.id)}>
                        <Github size={11} aria-hidden="true" />{t('clawhub.onGitHub')}
                      </button>
                    ) : (
                      <span className="ss-st off">{t('clawhub.notInstallable')}</span>
                    )
                  ) : have === it.version ? (
                    <span className="ss-st ok">{t('hubs.browse.installed')}</span>
                  ) : (
                    <button type="button" className="ss-btn sm" disabled={installing !== null} onClick={() => void install(it)}>
                      {installing === it.id ? <Spinner size="sm" /> : have ? t('clawhub.update') : t('hubs.browse.install')}
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
          {t('clawhub.showMore', { count: Math.min(PAGE, shown.length - limit), total: shown.length })}
        </button>
      )}
      {feed.data && (
        <span className="text-[12px] text-ink-3">
          {t('clawhub.feedInfo', { generated: date(feed.data.generatedAt), expires: date(feed.data.expiresAt) })}
        </span>
      )}

      {open && feed.data && (
        <ClawHubDetails
          item={open}
          registry={feed.data.registry}
          installedVersion={installed.get(open.id)}
          installing={installing === open.id}
          error={errors[open.id]}
          onInstall={() => void install(open)}
          onClose={() => setOpenID(null)}
        />
      )}
      <ConfirmDialog
        open={replace !== null}
        variant="danger"
        title={t('clawhub.replace.title', { name: replace?.slug ?? '' })}
        message={t('clawhub.replace.message', { name: replace?.slug ?? '', id: replace?.id ?? '', version: replace?.version ?? '' })}
        confirmText={t('clawhub.replace.confirm')}
        loading={replace !== null && installing === replace.id}
        onCancel={() => setReplace(null)}
        onConfirm={() => {
          const item = replace!;
          void install(item, true).then(() => setReplace(null));
        }}
      />
    </div>
  );
}
