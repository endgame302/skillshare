// Fork-only (endgame302/skillshare): a plugin plan's rows, drawn as upstream's review dialog on the Plugins page draws them.
import { Package } from 'lucide-react';
import type { PluginPlan, PluginTargetDefinition } from '../../../../api/plugins';
import AgentIcon from '../../../AgentIcon';
import { keyedMessage } from '../../../plugins/outcomeText';
import { useT } from '../../../../i18n';

// The action words upstream's review dialog uses (PluginsPage.tsx), as literal keys.
const ACTION_KEYS: Record<string, string> = {
  noop: 'plugins.noChanges',
  skip: 'plugins.outcome.skipped',
  install: 'resources.install',
  import: 'plugins.import',
  update: 'plugins.update',
  remove: 'plugins.remove',
  uninstall: 'plugins.remove',
  forget: 'plugins.remove',
  selection: 'common.save',
  record: 'plugins.record',
  blocked: 'plugins.blocked',
};

const actionTone = (action: string) => (action === 'blocked' ? 'bad' : action === 'noop' ? '' : action === 'skip' ? 'warn' : 'inf');

export default function PlanRows({ plan, definitions }: { plan: PluginPlan; definitions?: PluginTargetDefinition[] }) {
  const t = useT();
  if (plan.changes.length === 0) return <p className="text-[13px] text-ink-2">{t('plugins.noChanges')}</p>;
  // A change with no Agent is to skillshare itself: the plugin is only added to it.
  const label = (target: string) => definitions?.find((d) => d.target === target)?.label ?? (target || t('plugins.skillshareOnly'));
  return (
    <>
      <div className="ss-list">
        {plan.changes.map((c) => (
          <div key={`${c.name}:${c.target}`} className="ss-r">
            <span className="ss-at">{c.target ? <AgentIcon target={c.target} size={17} /> : <Package size={15} />}</span>
            <span className="flex min-w-0 flex-1 flex-col gap-0.5">
              <span className="flex flex-wrap items-center gap-x-2">
                <span className="font-mono font-semibold">{c.name}</span>
                <span className="text-[13px] text-ink-2">{label(c.target)}</span>
              </span>
              {c.action === 'record'
                ? <span className="text-xs text-ink-3">{t('plugins.recordHelp')}</span>
                : (!!c.message || !!c.components?.length) && <span className="text-xs text-ink-3">{c.message ? keyedMessage(t, c) : c.components!.join(' · ')}</span>}
            </span>
            <span className={`ss-tag ${c.failed ? 'bad' : actionTone(c.action)}`.trim()}>
              {c.failed ? t('plugins.outcome.failed') : t(ACTION_KEYS[c.action] ?? 'plugins.pending')}
            </span>
          </div>
        ))}
      </div>
      {plan.changes.some((c) => c.action === 'install' || c.action === 'update') && <p className="text-xs text-ink-3">{t('plugins.nativeHelp')}</p>}
    </>
  );
}
