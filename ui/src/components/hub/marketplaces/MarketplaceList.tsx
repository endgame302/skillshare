import { useT } from '../../../i18n';
import { MARKET, MARKETPLACES } from './registry';
import type { Marketplace } from './registry';

interface Props {
  selected: string | null;
  onPick: (key: string) => void;
  locked: boolean;
  /** For tests; the page uses every registered marketplace. */
  marketplaces?: Marketplace[];
}

/** The marketplaces under the hub list, in the same style (fork-only). */
export default function MarketplaceList({ selected, onPick, locked, marketplaces = MARKETPLACES }: Props) {
  const t = useT();
  if (marketplaces.length === 0) return null;
  return (
    <>
      <div className="ss-sec !mb-0 mt-3">
        <h2>{t('hubs.market.title')}</h2>
        <span className="ss-cnt">{marketplaces.length}</span>
      </div>
      <div className={`ss-list ${locked ? 'pointer-events-none opacity-55' : ''}`} aria-disabled={locked || undefined}>
        {marketplaces.map(({ id, labelKey, subKey, icon: Icon }) => {
          const key = MARKET + id;
          return (
            <button
              key={key}
              type="button"
              disabled={locked}
              aria-current={key === selected}
              className={`ss-r !min-h-[52px] w-full !flex-col !items-start !gap-0.5 text-start ${key === selected ? 'sel' : ''}`}
              onClick={() => onPick(key)}
            >
              <span className="flex items-center gap-1.5 text-[13.5px] font-semibold">
                <Icon size={13} aria-hidden="true" />
                {t(labelKey)}
              </span>
              <span className="w-full truncate text-[11.5px] text-ink-3">{t(subKey)}</span>
            </button>
          );
        })}
      </div>
    </>
  );
}
