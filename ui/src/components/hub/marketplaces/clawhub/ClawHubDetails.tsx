// Fork-only (endgame302/skillshare): one ClawHub skill, opened from the list.
import { ExternalLink, Github, ShieldCheck, Star } from 'lucide-react';
import type { ClawHubItem } from '../../../../api/forkClawhub';
import Button from '../../../Button';
import DialogShell from '../../../DialogShell';
import { useT } from '../../../../i18n';
import { blockedKey, trustLabel } from './labels';

interface Props {
  item: ClawHubItem;
  registry: string;
  installedVersion?: string;
  installing: boolean;
  error?: string;
  onInstall: () => void;
  onClose: () => void;
}

export default function ClawHubDetails({ item, registry, installedVersion, installing, error, onInstall, onClose }: Props) {
  const t = useT();
  const page = `${registry}/${encodeURIComponent(item.owner)}/skills/${encodeURIComponent(item.slug)}`;
  const gh = item.github;
  const ghURL = gh ? `https://github.com/${gh.repo}/tree/${gh.commit}/${gh.path}` : '';
  const rows: [string, React.ReactNode][] = [
    [t('clawhub.details.publisher'), (
      <span className="inline-flex items-center gap-1.5">
        <span className="font-mono">{item.publisher}</span>
        {item.trust && <span className="ss-tag ok" title={t('clawhub.trust.hint')}><ShieldCheck size={11} aria-hidden="true" />{trustLabel(t, item.trust)}</span>}
      </span>
    )],
    [t('clawhub.details.version'), (
      <span className="inline-flex items-center gap-2">
        <span className="font-mono">{gh && !item.installable ? gh.commit.slice(0, 12) : item.version}</span>
        {installedVersion && <span className="ss-st ok">{t('clawhub.details.installedVersion', { version: installedVersion })}</span>}
      </span>
    )],
    [t('clawhub.details.source'), gh && !item.installable ? (
      <a href={ghURL} target="_blank" rel="noopener noreferrer" className="inline-flex max-w-full items-center gap-1.5 hover:underline">
        <Github size={13} className="shrink-0" aria-hidden="true" />
        <span className="truncate font-mono">{gh.repo}/{gh.path}</span>
        <ExternalLink size={12} className="shrink-0 text-ink-3" aria-hidden="true" />
      </a>
    ) : t('clawhub.details.sourceHosted')],
  ];
  if (item.integrity) rows.push([t('clawhub.details.checksum'), <span className="break-all font-mono text-[12px]">{item.integrity}</span>]);

  return (
    <DialogShell open onClose={onClose} maxWidth="2xl" padding="none" preventClose={installing} ariaLabel={item.title}>
      <div className="dh sep">
        <div className="flex min-w-0 flex-col gap-1">
          <h2 className="ss-h2 flex items-center gap-2">
            <span className="truncate">{item.title}</span>
            {item.featured && <span className="ss-tag inf"><Star size={11} aria-hidden="true" />{t('clawhub.featured')}</span>}
          </h2>
          <span className="font-mono text-[12.5px] text-ink-3">{item.id}</span>
        </div>
      </div>
      <div className="db">
        {item.description && <p className="whitespace-pre-wrap break-words text-[13.5px] text-ink-2">{item.description}</p>}
        <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-2 text-[13px]">
          {rows.map(([label, value]) => (
            <div key={label} className="contents">
              <dt className="text-ink-3">{label}</dt>
              <dd className="min-w-0">{value}</dd>
            </div>
          ))}
        </dl>
        <div className="ss-note warn"><span className="flex-1">{t('clawhub.thirdParty')}</span></div>
        {!item.installable && item.blocked && <div className="ss-note inf"><span className="flex-1">{t(blockedKey[item.blocked])}</span></div>}
        {error && <div role="alert" className="ss-note bad"><span className="flex-1 break-words">{error}</span></div>}
      </div>
      <div className="df">
        <a href={page} target="_blank" rel="noopener noreferrer" className="ss-btn ghost mr-auto inline-flex items-center gap-1.5">
          {t('clawhub.details.openClawHub')}<ExternalLink size={12} aria-hidden="true" />
        </a>
        {gh && !item.installable && (
          <a href={ghURL} target="_blank" rel="noopener noreferrer" className="ss-btn inline-flex items-center gap-1.5">
            <Github size={13} aria-hidden="true" />{t('clawhub.details.openGitHub')}
          </a>
        )}
        <Button variant="ghost" onClick={onClose} disabled={installing}>{t('common.close')}</Button>
        {item.installable && (
          installedVersion === item.version ? (
            <span className="ss-st ok">{t('hubs.browse.installed')}</span>
          ) : (
            <Button onClick={onInstall} loading={installing}>{installedVersion ? t('clawhub.update') : t('hubs.browse.install')}</Button>
          )
        )}
      </div>
    </DialogShell>
  );
}
