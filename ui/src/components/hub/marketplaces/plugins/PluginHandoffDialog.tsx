// Fork-only (endgame302/skillshare): installs a plugin hosted outside its catalog with Claude Code's own CLI, after the
// user has read the exact commands, then imports what Claude installed into skillshare.
import { useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { ExternalLink, X } from 'lucide-react';
import type { CatalogEntry, HandoffPlan, PluginCatalog } from '../../../../api/forkPlugins';
import { forkPlugins } from '../../../../api/forkPlugins';
import { pluginsApi } from '../../../../api/plugins';
import type { PluginPlan, PluginRequest } from '../../../../api/plugins';
import Button from '../../../Button';
import DialogShell from '../../../DialogShell';
import IconButton from '../../../IconButton';
import Spinner from '../../../Spinner';
import { useToast } from '../../../Toast';
import { pluginErrorMessage } from '../../../plugins/pluginError';
import { failureText, importRequest } from '../../../plugins/pluginsFlow';
import { useT } from '../../../../i18n';
import { invalidate } from '../../../../lib/queryEvents';
import { queryKeys } from '../../../../lib/queryKeys';
import { commandLine, handoffErrorText } from './helpers';
import PlanRows from './PlanRows';

interface Props {
  catalog: PluginCatalog;
  entry: CatalogEntry;
  onClose: () => void;
}

/** Claude Code's documented installers, for a computer without the claude command. */
const CLAUDE_INSTALLERS: { key: string; command: string }[] = [
  { key: 'pluginsMarket.handoff.missingWindows', command: 'irm https://claude.ai/install.ps1 | iex' },
  { key: 'pluginsMarket.handoff.missingUnix', command: 'curl -fsSL https://claude.ai/install.sh | bash' },
  { key: 'pluginsMarket.handoff.missingNpm', command: 'npm install -g @anthropic-ai/claude-code@latest' },
];
const SETUP_GUIDE = 'https://code.claude.com/docs/en/setup';

export default function PluginHandoffDialog({ catalog, entry, onClose }: Props) {
  const t = useT();
  const queryClient = useQueryClient();
  const { toast } = useToast();
  const [running, setRunning] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  // Set once Claude Code has installed it: the dialog then imports it into skillshare.
  const [installed, setInstalled] = useState<{ pluginId: string; message?: string } | null>(null);
  const [review, setReview] = useState<{ request: PluginRequest; plan: PluginPlan } | null>(null);
  const [importFailed, setImportFailed] = useState(false);

  const body = { catalog: catalog.id, name: entry.name, commit: catalog.commit, sha: entry.source.sha ?? '' };
  // The dry run only asks claude where it is, its version and its marketplaces; nothing is changed.
  const dry = useQuery({
    queryKey: ['fork', 'plugins', 'handoff-plan', catalog.id, catalog.commit, entry.name, body.sha] as const,
    queryFn: () => forkPlugins.handoff({ ...body, dryRun: true }),
    staleTime: 0,
    gcTime: 0,
    retry: false,
  });
  const plan: HandoffPlan | undefined = dry.data?.plan;
  // The list's own query: the Agents' names for the import rows.
  const inventory = useQuery({ queryKey: queryKeys.pluginPackages, queryFn: () => pluginsApi.list(false) });

  const previewImport = async (pluginId: string) => {
    setBusy(true);
    setError('');
    try {
      const request = importRequest('claude', pluginId);
      setReview({ request, plan: await pluginsApi.preview(request) });
    } catch (e) {
      setError(pluginErrorMessage(e, t));
      setImportFailed(true);
    } finally {
      setBusy(false);
    }
  };

  const run = async () => {
    setRunning(true);
    setBusy(true);
    setError('');
    try {
      const res = await forkPlugins.handoff({ ...body, dryRun: false });
      const pluginId = res.result?.pluginId ?? res.plan.pluginId;
      setInstalled({ pluginId, message: res.result?.message });
      void invalidate(queryClient, 'pluginsChanged');
      setRunning(false);
      await previewImport(pluginId);
    } catch (e) {
      setError(handoffErrorText(t, e));
    } finally {
      setRunning(false);
      setBusy(false);
    }
  };

  const apply = async () => {
    if (!review || !installed) return;
    setBusy(true);
    setError('');
    try {
      const response = await pluginsApi.apply(review.request, review.plan.revision);
      const failed = failureText(t, response);
      if (failed) {
        setError(failed);
        setImportFailed(true);
        setReview(null);
      } else {
        toast(t('pluginsMarket.toast.handedOff', { id: installed.pluginId }), 'success');
        onClose();
      }
    } catch (e) {
      setError(pluginErrorMessage(e, t));
      setImportFailed(true);
    } finally {
      setBusy(false);
      void invalidate(queryClient, 'pluginsChanged');
    }
  };

  const title = t('pluginsMarket.handoff.title', { name: entry.displayName || entry.name });
  const sha = entry.source.sha;
  return (
    <DialogShell open onClose={onClose} preventClose={busy} ariaLabel={title} maxWidth="2xl" padding="none">
      <div className="dh">
        <div className="flex min-w-0 flex-col gap-1">
          <h2 className="ss-h2">{title}</h2>
          <p className="truncate font-mono text-xs text-ink-3">{entry.name}@{catalog.market}</p>
        </div>
        <IconButton icon={<X size={16} />} label={t('common.close')} disabled={busy} onClick={onClose} />
      </div>
      <div className="db overflow-y-auto">
        {dry.isPending && (
          <p className="flex items-center gap-2 text-[13px] text-ink-2"><Spinner size="sm" />{t('pluginsMarket.handoff.checking')}</p>
        )}
        {dry.isError && (
          <div role="alert" className="ss-note bad"><span className="flex-1 break-words">{handoffErrorText(t, dry.error)}</span></div>
        )}

        {plan && plan.claude === '' && (
          <>
            <p className="ss-note warn">{t('pluginsMarket.handoff.missing')}</p>
            <ul className="flex flex-col gap-2">
              {CLAUDE_INSTALLERS.map((i) => (
                <li key={i.command} className="flex flex-col gap-1">
                  <span className="text-[13px] text-ink-2">{t(i.key)}</span>
                  <pre className="ss-code !whitespace-pre-wrap break-all !px-3 !py-2">{i.command}</pre>
                </li>
              ))}
            </ul>
            <a href={SETUP_GUIDE} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 self-start text-[13px] underline hover:text-ink">
              {t('pluginsMarket.handoff.setupGuide')}<ExternalLink size={12} aria-hidden="true" />
            </a>
          </>
        )}

        {plan && plan.claude !== '' && !installed && (
          <>
            <p className="text-[13px] text-ink-2">{t('pluginsMarket.handoff.intro')}</p>
            <pre className="ss-code !whitespace-pre-wrap break-all !px-3 !py-2.5">
              {plan.steps.map((s) => commandLine(s.args)).join('\n')}
            </pre>
            <p className="text-[13px] text-ink-2">{t('pluginsMarket.handoff.then')}</p>
            <pre className="ss-code !whitespace-pre-wrap break-all !px-3 !py-2.5">{plan.import}</pre>
            {sha
              ? <p className="text-[13px] text-ink-2">{t('pluginsMarket.handoff.pinned', { sha: sha.slice(0, 7) })}</p>
              : <p className="ss-note warn">{t('pluginsMarket.handoff.unpinned')}</p>}
            {plan.registered && <p className="text-[13px] text-ink-2">{t('pluginsMarket.handoff.registered', { market: plan.market })}</p>}
            <p className="ss-note warn">{t('pluginsMarket.handoff.trust')}</p>
            {plan.claudeVersion && <p className="text-xs text-ink-3">{t('pluginsMarket.handoff.version', { version: plan.claudeVersion })}</p>}
            {running && (
              <p className="flex items-center gap-2 text-[13px] text-ink-2"><Spinner size="sm" />{t('pluginsMarket.handoff.running', { id: plan.pluginId })}</p>
            )}
          </>
        )}

        {installed && (
          <>
            <p className="ss-note ok">{t('pluginsMarket.handoff.done', { id: installed.pluginId })}</p>
            {installed.message && <p className="text-xs text-ink-3">{installed.message}</p>}
            {busy && !review && <p className="flex items-center gap-2 text-[13px] text-ink-2"><Spinner size="sm" />{t('pluginsMarket.handoff.importing')}</p>}
            {review && (
              <>
                <p className="text-[13px] text-ink-2">{t('pluginsMarket.handoff.importHelp')}</p>
                <PlanRows plan={review.plan} definitions={inventory.data?.targetDefinitions} />
              </>
            )}
            {importFailed && <p className="text-[13px] text-ink-2">{t('pluginsMarket.handoff.importLater')}</p>}
          </>
        )}

        {error && <div role="alert" className="ss-note bad"><span className="flex-1 break-words">{error}</span></div>}
      </div>
      <div className="df">
        <Button variant="ghost" disabled={busy} onClick={onClose}>{installed ? t('common.close') : t('common.cancel')}</Button>
        {!installed && (dry.isError || plan?.claude === '') && (
          <Button variant="secondary" loading={dry.isFetching} onClick={() => { setError(''); void dry.refetch(); }}>{t('pluginsMarket.retry')}</Button>
        )}
        {!installed && plan && plan.claude !== '' && (
          <Button loading={running} disabled={busy || dry.isFetching} onClick={() => void run()}>{t('pluginsMarket.handoff.run')}</Button>
        )}
        {installed && review && !importFailed && (
          <Button loading={busy} disabled={review.plan.blocked || review.plan.changes.length === 0} onClick={() => void apply()}>{t('plugins.apply')}</Button>
        )}
      </div>
    </DialogShell>
  );
}
