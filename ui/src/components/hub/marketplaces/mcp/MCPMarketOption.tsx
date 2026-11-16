// Fork-only (endgame302/skillshare): one install option of an MCP marketplace entry: what it runs,
// what it asks for, and what to know before adding it.
import { Globe, SquareTerminal } from 'lucide-react';
import type { MCPMarketOption } from '../../../../api/forkMcpMarket';
import { Select } from '../../../Select';
import { useT } from '../../../../i18n';
import { commandLine, envUse, warningText } from './labels';

interface Props {
  option: MCPMarketOption;
  values: Record<string, string>;
  onValue: (key: string, value: string) => void;
  disabled: boolean;
}

export default function MCPMarketOptionFields({ option: o, values, onValue, disabled }: Props) {
  const t = useT();
  const missing = new Set(o.missing ?? []);
  return (
    <>
      {o.command && o.command.length > 0 && (
        <div className="ss-fld">
          <span className="text-[13px] font-semibold">{t('mcpmarket.install.runs')}</span>
          <span className="ss-inp font-mono !h-auto !bg-sunken py-1.5">
            <SquareTerminal size={15} className="shrink-0 self-start text-ink-3" />
            <code className="break-all">{commandLine(o.command)}</code>
          </span>
        </div>
      )}
      {o.url && !o.command && (
        <div className="ss-fld">
          <span className="text-[13px] font-semibold">{t('mcpmarket.install.connects')}</span>
          <span className="ss-inp font-mono !h-auto !bg-sunken py-1.5">
            <Globe size={15} className="shrink-0 self-start text-ink-3" />
            <code className="break-all">{o.url}</code>
          </span>
          {o.host && <span className="hp">{t('mcpmarket.install.host', { host: o.host, domain: o.domain ?? o.host })}</span>}
        </div>
      )}

      {(o.inputs ?? []).length > 0 && (
        <div className="flex flex-col gap-2.5">
          <span className="text-[13px] font-semibold">{t('mcpmarket.install.inputs')}</span>
          {o.inputs!.map((f) => {
            const id = `mcpmarket-input-${f.key}`;
            const label = `${f.label}${f.required ? ' *' : ''}`;
            const value = values[f.key] ?? f.value ?? '';
            return (
              <div key={f.key} className="ss-fld">
                {f.choices && f.choices.length > 0 ? (
                  <Select
                    label={label}
                    value={value}
                    onChange={(v) => onValue(f.key, v)}
                    options={[...(f.required ? [] : [{ value: '', label: t('mcpmarket.install.none') }]), ...f.choices.map((c) => ({ value: c, label: c }))]}
                    disabled={disabled}
                    invalid={missing.has(f.key)}
                  />
                ) : (
                  <>
                    <label htmlFor={id}>{label}</label>
                    <span className={`ss-inp font-mono ${missing.has(f.key) ? 'err' : ''}`}>
                      <input id={id} value={value} placeholder={f.default} onChange={(e) => onValue(f.key, e.target.value)} disabled={disabled} />
                    </span>
                  </>
                )}
                {f.description && <span className="hp">{f.description}</span>}
                {missing.has(f.key) && <span className="hp !text-bad">{f.required && !value ? t('mcpmarket.install.required') : t('mcpmarket.install.invalidChoice')}</span>}
              </div>
            );
          })}
        </div>
      )}

      {(o.envVars ?? []).length > 0 && (
        <div className="flex flex-col gap-1.5">
          <span className="text-[13px] font-semibold">{t('mcpmarket.install.envTitle')}</span>
          <span className="text-[12.5px] text-ink-2">{t('mcpmarket.install.envHint')}</span>
          <ul className="m-0 flex list-none flex-col gap-1 p-0 text-[13px]">
            {o.envVars!.map((v) => (
              <li key={`${v.name}:${v.use}`} className="flex flex-wrap items-baseline gap-x-2">
                <code className="font-mono font-semibold">{v.name}</code>
                <span className="text-ink-3">{envUse(t, v.use)}</span>
                {v.description && <span className="min-w-0 text-ink-2">— {v.description}</span>}
              </li>
            ))}
          </ul>
        </div>
      )}
      {(o.skipped ?? []).length > 0 && (
        <p className="m-0 text-[12.5px] text-ink-2">{t('mcpmarket.install.skipped', { names: o.skipped!.map((v) => v.name).join(', ') })}</p>
      )}

      {(o.warnings ?? []).map((w, i) => (
        <div key={`${w.code}:${i}`} className="ss-note warn"><span className="flex-1 break-words">{warningText(t, w)}</span></div>
      ))}
    </>
  );
}
