// Fork-only (endgame302/skillshare): add one MCP marketplace entry to skillshare. The entry becomes
// a skillshare MCP server, the user reviews what skillshare will write, and it is saved through
// the MCP page's own API; Sync then writes it into the Agents' files as usual.
import { useState } from 'react';
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query';
import { CircleCheck, ExternalLink } from 'lucide-react';
import { Link, useNavigate } from 'react-router-dom';
import type { MCPMarketEntry, MCPMarketOption, MCPMarketSource } from '../../../../api/forkMcpMarket';
import { forkMcpMarket, mcpMarketKey } from '../../../../api/forkMcpMarket';
import { mcpApi, mcpTargets } from '../../../../api/mcp';
import type { MCPMutation, MCPPlan, MCPServer } from '../../../../api/mcp';
import AgentIcon from '../../../AgentIcon';
import Button from '../../../Button';
import { Checkbox } from '../../../Checkbox';
import DialogShell from '../../../DialogShell';
import { Select } from '../../../Select';
import Spinner from '../../../Spinner';
import { useToast } from '../../../Toast';
import MCPConfigView from '../../../mcp/MCPConfigView';
import { MCPSyncDialog } from '../../../mcp/MCPSyncBox';
import { targetLabel, writes } from '../../../mcp/mcpView';
import { useMcpQuery } from '../../../../hooks/useSharedQueries';
import { useT } from '../../../../i18n';
import { invalidate } from '../../../../lib/queryEvents';
import MCPMarketOptionFields from './MCPMarketOption';
import { mcpMarketErrorText, refusalText } from './labels';
import { useDebounced } from './useDebounced';

const NAME = /^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$/;
const known: readonly string[] = mcpTargets;

interface Props {
  source: MCPMarketSource;
  entry: MCPMarketEntry;
  onClose: () => void;
}

export default function MCPMarketInstall({ source, entry, onClose }: Props) {
  const t = useT();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { toast } = useToast();
  const [values, setValues] = useState<Record<string, string>>({});
  const applied = useDebounced(values, 400);
  const plan = useQuery({
    queryKey: [...mcpMarketKey, 'plan', source, entry.name, applied],
    queryFn: () => forkMcpMarket.plan(source, entry.name, applied),
    placeholderData: keepPreviousData,
    gcTime: 0,
    retry: false,
  });
  const mcp = useMcpQuery();
  const [optionID, setOptionID] = useState<string | null>(null);
  const [nameDraft, setNameDraft] = useState<string | null>(null);
  const [useDefaults, setUseDefaults] = useState(true);
  const [custom, setCustom] = useState<string[] | null>(null);
  const [replace, setReplace] = useState(false);
  const [review, setReview] = useState<MCPMutation | null>(null);
  const [step, setStep] = useState<'form' | 'review' | 'done'>('form');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [syncPlan, setSyncPlan] = useState<MCPPlan | null>(null);

  const p = plan.data;
  const options = p?.options ?? [];
  const option: MCPMarketOption | undefined = options.find((o) => o.id === optionID) ?? options.find((o) => !o.refused) ?? options[0];
  const name = nameDraft ?? option?.name ?? '';
  const nameOK = NAME.test(name);
  const exists = Boolean(mcp.data?.source.servers?.[name]);
  const defaults = mcp.data?.source.targets ?? [];
  const inherit = defaults.length > 0 && useDefaults;
  const customTargets = custom ?? (mcp.data?.detected ?? []).filter((x) => known.includes(x));
  const targets = inherit ? defaults : customTargets;
  const windowsNote = p?.os === 'windows' && (option?.runtime === 'npx' || option?.runtime === 'uvx') && targets.some((x) => x === 'codex' || x === 'gemini');
  const title = entry.title || entry.name;
  const canReview = Boolean(option && !option.refused && option.server && (option.missing ?? []).length === 0 && nameOK && (!exists || replace) && targets.length > 0 && mcp.data && !plan.isFetching);

  const toggle = (target: string) => setCustom(customTargets.includes(target) ? customTargets.filter((x) => x !== target) : [...customTargets, target]);

  // The plan is asked for once more with the values as typed, so what is reviewed is what is saved.
  const toReview = async () => {
    if (!option) return;
    setBusy(true);
    setError('');
    try {
      const fresh = (await forkMcpMarket.plan(source, entry.name, values)).options.find((o) => o.id === option.id);
      if (!fresh?.server) throw new Error(fresh?.refused ? refusalText(t, fresh.refused, fresh.detail) : t('mcpmarket.install.incomplete'));
      const server: MCPServer = { ...fresh.server, ...(!inherit && { targets }) };
      setReview({ name, server, ...(exists && { replace: true }) });
      setStep('review');
    } catch (e) {
      setError(mcpMarketErrorText(t, e));
    } finally {
      setBusy(false);
    }
  };

  const add = async () => {
    if (!review) return;
    setBusy(true);
    setError('');
    try {
      await mcpApi.save(review);
      toast(t('mcpmarket.install.added', { name: review.name ?? '' }), 'success');
      void invalidate(queryClient, 'mcpChanged');
      setStep('done');
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const sync = async () => {
    setBusy(true);
    setError('');
    try {
      const next = await mcpApi.preview();
      // A blocked plan applies nothing; the Sync page says why.
      if (next.blocked) navigate('/sync');
      else setSyncPlan(next);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  if (syncPlan) return <MCPSyncDialog plan={syncPlan} shown={syncPlan.changes.filter(writes).length} onClose={onClose} />;

  // What Sync would write per Agent: the inherited targets are shown, though the server keeps inheriting them.
  const shown = review && { ...review, server: { ...review.server, targets: review.server?.targets ?? targets } };

  return (
    <DialogShell open onClose={onClose} maxWidth="2xl" padding="none" preventClose={busy} ariaLabel={title}>
      <div className="dh sep">
        <div className="flex min-w-0 flex-col gap-1">
          <h2 className="ss-h2 truncate">{step === 'review' ? t('mcpmarket.install.reviewTitle', { name }) : title}</h2>
          <span className="truncate font-mono text-[12.5px] text-ink-3">{entry.name}{entry.version && ` · ${entry.version}`}</span>
        </div>
      </div>

      {step === 'done' && review ? (
        <div className="db">
          <div className="ss-note inf"><CircleCheck size={16} /><span className="flex-1">{t('mcpmarket.install.doneNote', { name: review.name ?? '' })}</span></div>
        </div>
      ) : step === 'review' && shown ? (
        <div className="db">
          <p className="m-0 text-[13px] text-ink-2">
            {t('mcpmarket.install.reviewIntro', { targets: targets.map(targetLabel).join(', ') })}
            {inherit && ` ${t('mcpmarket.install.reviewInherit')}`}
          </p>
          <MCPConfigView mutation={shown} />
        </div>
      ) : (
        <div className="db">
          {entry.description && <p className="m-0 whitespace-pre-wrap break-words text-[13.5px] text-ink-2">{entry.description}</p>}
          {(entry.repository || entry.website) && (
            <span className="flex flex-wrap gap-x-4 gap-y-1 text-[13px]">
              {entry.repository && <a href={entry.repository} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 hover:underline">{t('mcpmarket.install.repository')}<ExternalLink size={12} aria-hidden="true" /></a>}
              {entry.website && <a href={entry.website} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 hover:underline">{t('mcpmarket.install.website')}<ExternalLink size={12} aria-hidden="true" /></a>}
            </span>
          )}
          {plan.isPending && <div className="flex items-center gap-2.5 text-[13px] text-ink-2"><Spinner size="sm" />{t('mcpmarket.install.planning')}</div>}
          {plan.isError && !p && <div role="alert" className="ss-note bad"><span className="flex-1 break-words">{mcpMarketErrorText(t, plan.error)}</span></div>}
          {options.length > 1 && option && (
            <Select
              label={t('mcpmarket.install.option')}
              value={option.id}
              onChange={(v) => { setOptionID(v); setNameDraft(null); }}
              options={options.map((o) => ({ value: o.id, label: o.refused ? t('mcpmarket.install.optionRefused', { label: o.label }) : o.label }))}
              disabled={busy}
            />
          )}
          {option?.refused && <div className="ss-note bad"><span className="flex-1 break-words">{refusalText(t, option.refused, option.detail)}</span></div>}
          {option?.refused && entry.repository && <p className="m-0 text-[13px] text-ink-2">{t('mcpmarket.install.refusedSource')}</p>}
          {option && !option.refused && (
            <div className="ss-fld">
              <label htmlFor="mcpmarket-name">{t('mcpmarket.install.name')}</label>
              <span className={`ss-inp font-mono ${nameOK ? '' : 'err'}`}>
                <input id="mcpmarket-name" value={name} onChange={(e) => setNameDraft(e.target.value)} disabled={busy} />
              </span>
              {!nameOK && <span className="hp !text-bad">{t('mcpmarket.install.badName')}</span>}
            </div>
          )}
          {option && !option.refused && exists && nameOK && (
            <div className="ss-note warn">
              <span className="flex flex-1 flex-col gap-1.5">
                {t('mcpmarket.install.exists', { name })}
                <Checkbox label={t('mcpmarket.install.replace')} checked={replace} onChange={setReplace} disabled={busy} />
              </span>
            </div>
          )}
          {option && <MCPMarketOptionFields option={option} values={values} onValue={(k, v) => setValues((m) => ({ ...m, [k]: v }))} disabled={busy} />}
          {option && !option.refused && windowsNote && (
            <div className="ss-note inf"><span className="flex-1">{t('mcpmarket.install.windowsNpx', { runtime: option.runtime ?? '' })}</span></div>
          )}
          {option && !option.refused && mcp.data && (
            <div className="flex flex-col gap-2">
              <span className="text-[13px] font-semibold">{t('mcpmarket.install.targets')}</span>
              {defaults.length > 0 ? (
                <Checkbox label={t('mcpmarket.install.useDefaults', { targets: defaults.map(targetLabel).join(', ') })} checked={useDefaults} onChange={setUseDefaults} disabled={busy} />
              ) : (
                <span className="text-[12.5px] text-ink-2">{t('mcpmarket.install.noDefaults')}</span>
              )}
              {!inherit && (
                <div className="grid grid-cols-[repeat(auto-fill,minmax(150px,1fr))] gap-1.5">
                  {mcpTargets.map((x) => (
                    <span key={x} className="flex items-center gap-1.5">
                      <AgentIcon target={x} size={14} />
                      <Checkbox label={targetLabel(x)} checked={customTargets.includes(x)} onChange={() => toggle(x)} disabled={busy} />
                    </span>
                  ))}
                </div>
              )}
              {!inherit && targets.length === 0 && <span className="hp !text-bad">{t('mcpmarket.install.pickTarget')}</span>}
            </div>
          )}
        </div>
      )}
      {error && <div className="px-6 pb-3"><div role="alert" className="ss-note bad"><span className="flex-1 break-words">{error}</span></div></div>}

      <div className="df">
        <Link to="/mcp" className="ss-more order-first mr-auto" onClick={onClose}>{t('mcpmarket.install.openMcp')}</Link>
        {step === 'form' && (
          <>
            <Button variant="ghost" onClick={onClose} disabled={busy}>{t(option?.refused ? 'common.close' : 'common.cancel')}</Button>
            {option && !option.refused && <Button onClick={() => void toReview()} loading={busy} disabled={!canReview}>{t('mcpmarket.install.review')}</Button>}
          </>
        )}
        {step === 'review' && (
          <>
            <Button variant="ghost" onClick={() => setStep('form')} disabled={busy}>{t('common.back')}</Button>
            <Button onClick={() => void add()} loading={busy}>{t('mcpmarket.install.add')}</Button>
          </>
        )}
        {step === 'done' && (
          <>
            <Button variant="ghost" onClick={onClose} disabled={busy}>{t('common.close')}</Button>
            <Button onClick={() => void sync()} loading={busy}>{t('mcpmarket.install.syncNow')}</Button>
          </>
        )}
      </div>
    </DialogShell>
  );
}
