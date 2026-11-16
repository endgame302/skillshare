// Fork-only (endgame302/skillshare): installs a plugin stored in its catalog's repo through skillshare's own plugin add,
// pinned to the catalog commit the list was read at.
import { useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Check, X } from 'lucide-react';
import type { CatalogEntry, PluginCatalog } from '../../../../api/forkPlugins';
import { pluginsApi, targetMap } from '../../../../api/plugins';
import type { PluginPlan, PluginRequest, PluginTarget } from '../../../../api/plugins';
import AgentIcon from '../../../AgentIcon';
import Button from '../../../Button';
import DialogShell from '../../../DialogShell';
import IconButton from '../../../IconButton';
import Spinner from '../../../Spinner';
import { useToast } from '../../../Toast';
import { agentReasons } from '../../../plugins/agentReasons';
import { keyedText } from '../../../plugins/outcomeText';
import { pluginErrorMessage } from '../../../plugins/pluginError';
import { failureText } from '../../../plugins/pluginsFlow';
import SourceHint from '../../../plugins/SourceHint';
import { useAppContext } from '../../../../context/AppContext';
import { useT } from '../../../../i18n';
import { invalidate } from '../../../../lib/queryEvents';
import PlanRows from './PlanRows';

interface Props {
  catalog: PluginCatalog;
  entry: CatalogEntry;
  onClose: () => void;
}

/** Discovery of one catalog commit; the commit pins it, so a second plugin from the same catalog reuses it. */
const discoveryKey = (repo: string, commit: string) => ['fork', 'plugins', 'discover', repo, commit] as const;

export default function PluginAddFlow({ catalog, entry, onClose }: Props) {
  const t = useT();
  const { isProjectMode } = useAppContext();
  const queryClient = useQueryClient();
  const { toast } = useToast();
  const [targets, setTargets] = useState<PluginTarget[]>([]);
  const [review, setReview] = useState<{ request: PluginRequest; plan: PluginPlan } | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const discovery = useQuery({
    queryKey: discoveryKey(catalog.repo, catalog.commit),
    queryFn: () => pluginsApi.discover(catalog.repo, catalog.commit),
    staleTime: 10 * 60_000,
    retry: false,
  });
  const candidate = discovery.data?.candidates.find((c) => c.name === entry.name);
  const definitions = discovery.data?.targetDefinitions;
  const reasons = candidate ? agentReasons(candidate, targetMap(definitions), isProjectMode, t) : [];
  const usable = reasons.filter((r) => !r.reason);
  const blocked = reasons.filter((r) => r.reason);
  const problem = candidate ? keyedText(t, candidate.problemKey, candidate.problem, candidate.problemArgs) : '';
  const missing = discovery.data && !candidate;
  const working = busy || discovery.isFetching;

  const preview = async () => {
    if (!discovery.data) return;
    setBusy(true);
    setError('');
    try {
      const request: PluginRequest = {
        action: 'add',
        source: discovery.data.source,
        sourceRef: discovery.data.sourceRef || catalog.commit,
        plugin: entry.name,
        targets,
      };
      setReview({ request, plan: await pluginsApi.preview(request) });
    } catch (e) {
      setError(pluginErrorMessage(e, t));
    } finally {
      setBusy(false);
    }
  };

  const apply = async () => {
    if (!review) return;
    setBusy(true);
    setError('');
    try {
      const response = await pluginsApi.apply(review.request, review.plan.revision);
      const failed = failureText(t, response);
      if (failed) {
        setError(failed);
        setReview(null);
      } else {
        toast(t('pluginsMarket.toast.installed', { name: entry.name }), 'success');
        onClose();
      }
    } catch (e) {
      setError(pluginErrorMessage(e, t));
    } finally {
      setBusy(false);
      void invalidate(queryClient, 'pluginsChanged');
    }
  };

  const title = t('pluginsMarket.add.title', { name: entry.displayName || entry.name });
  return (
    <DialogShell open onClose={onClose} preventClose={busy} ariaLabel={title} maxWidth="2xl" padding="none">
      <div className="dh">
        <div className="flex min-w-0 flex-col gap-1">
          <h2 className="ss-h2">{title}</h2>
          <p className="truncate font-mono text-xs text-ink-3">{catalog.repo} · {catalog.commit.slice(0, 7)}</p>
        </div>
        <IconButton icon={<X size={16} />} label={t('common.close')} disabled={busy} onClick={onClose} />
      </div>
      <div className="db overflow-y-auto">
        {discovery.isPending && (
          <div className="flex flex-col gap-1.5">
            <p className="flex items-center gap-2 text-[13px] text-ink-2"><Spinner size="sm" />{t('plugins.discovering')}</p>
            <SourceHint active />
          </div>
        )}
        {discovery.isError && (
          <div role="alert" className="ss-note bad"><span className="flex-1 break-words">{pluginErrorMessage(discovery.error, t)}</span></div>
        )}
        {missing && (
          <div role="alert" className="ss-note bad"><span className="flex-1">{t('pluginsMarket.add.missing', { name: entry.name })}</span></div>
        )}
        {candidate && problem && (
          <div role="alert" className="ss-note bad"><span className="flex-1 break-words">{problem}</span></div>
        )}
        {review ? (
          <>
            <p className="text-[13px] text-ink-2">{t('plugins.previewHelp')}</p>
            <PlanRows plan={review.plan} definitions={definitions} />
            {review.plan.blocked && <p className="ss-note warn">{t('pluginsMarket.add.blocked')}</p>}
          </>
        ) : candidate && !problem && (
          <>
            {discovery.data?.warnings?.map((w) => <p key={w} className="ss-note warn">{w}</p>)}
            {candidate.description && <p className="text-[13px] text-ink-2">{candidate.description}</p>}
            <div className="ss-fld">
              <span className="text-[13px] font-semibold">{t('plugins.targets')}</span>
              {usable.length === 0 ? (
                <p className="text-[13px] text-ink-3">{t('pluginsMarket.add.noTargets')}</p>
              ) : (
                <div className="grid grid-cols-3 gap-x-4 gap-y-3.5 pt-1">
                  {usable.map(({ target, definition }) => {
                    const on = targets.includes(target);
                    return (
                      <button
                        key={target}
                        type="button"
                        role="checkbox"
                        aria-checked={on}
                        aria-label={definition.label}
                        disabled={busy}
                        className={`ss-tgl ${on ? 'on' : ''}`}
                        onClick={() => setTargets((old) => (on ? old.filter((x) => x !== target) : [...old, target]))}
                      >
                        <span className="ic"><AgentIcon target={target} size={20} /><i><Check size={9} strokeWidth={3.5} /></i></span>
                        {definition.label}
                      </button>
                    );
                  })}
                </div>
              )}
              {blocked.length > 0 && (
                <div className="mt-1.5 flex flex-col gap-1.5">
                  <span className="flex items-baseline gap-2">
                    <span className="text-[13px] font-semibold text-ink-2">{t('plugins.targetsBlocked')}</span>
                    <span className="ss-cnt">{blocked.length}</span>
                  </span>
                  <div className="ss-list !shadow-none">
                    {blocked.map(({ target, definition, reason }) => (
                      <div key={target} className="ss-r !min-h-9">
                        <span className="ss-at opacity-55"><AgentIcon target={target} size={15} /></span>
                        <span className="w-36 shrink-0 truncate text-[13px] text-ink-2">{definition.label}</span>
                        <span className="min-w-0 flex-1 text-xs text-ink-3">{reason}</span>
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>
          </>
        )}
        {busy && <SourceHint active />}
        {error && <div role="alert" className="ss-note bad"><span className="flex-1 break-words">{error}</span></div>}
      </div>
      <div className="df">
        {review && <Button variant="ghost" className="mr-auto" disabled={busy} onClick={() => setReview(null)}>{t('common.back')}</Button>}
        {!review && candidate && !problem && targets.length === 0 && <span className="text-xs text-ink-3">{t('plugins.targetsOptional')}</span>}
        <Button variant="ghost" disabled={busy} onClick={onClose}>{t('common.cancel')}</Button>
        {review ? (
          <Button loading={busy} disabled={review.plan.blocked || review.plan.changes.length === 0} onClick={() => void apply()}>{t('plugins.apply')}</Button>
        ) : (
          <Button loading={working} disabled={!candidate || !!problem} onClick={() => void preview()}>{t('plugins.preview')}</Button>
        )}
      </div>
    </DialogShell>
  );
}
