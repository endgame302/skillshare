import { useQuery } from '@tanstack/react-query';
import { api } from '../../api/client';
import { forkPlainHub } from '../../api/forkPlainHub';
import { queryKeys } from '../../lib/queryKeys';
import { useT } from '../../i18n';
import { isPlainRepoHub } from './plainRepo';

// Fork-only (endgame302/skillshare): a plain GitHub repo of skills saved as a hub. The server
// lists its SKILL.md files itself, since the repo has no skillshare-hub.json.

/** Marks a hub row whose list is built from the repo rather than read from a hub file. */
export function PlainRepoTag({ url }: { url: string }) {
  const t = useT();
  if (!isPlainRepoHub(url)) return null;
  return <span className="ss-tag" title={t('plainhub.tagHint')}>{t('plainhub.tag')}</span>;
}

/** Says where a repo hub's list came from, and when the repo had more skills than it lists. */
export function PlainRepoNote({ url }: { url: string }) {
  const t = useT();
  const plain = isPlainRepoHub(url);
  // Follows the hub view's own list query without fetching it, so the note is read again
  // each time the list is, refresh included.
  const contents = useQuery({
    queryKey: queryKeys.hub.contents(url),
    queryFn: () => api.searchHub('', url),
    enabled: false,
  });
  const info = useQuery({
    queryKey: ['fork', 'plainhub', url, contents.dataUpdatedAt] as const,
    queryFn: () => forkPlainHub.info(url),
    enabled: plain && contents.dataUpdatedAt > 0,
  });
  // A repo that could not be read shows the view's error instead.
  if (!plain || contents.isError) return null;
  const meta = info.data;
  return (
    <span className="text-[12.5px] text-ink-3">
      {meta?.repo && meta.commit ? t('plainhub.noteAt', { repo: meta.repo, commit: meta.commit }) : t('plainhub.note')}
      {meta?.capped && (
        <span className="text-warn"> {t('plainhub.capped', { listed: meta.listed ?? 0, total: meta.total ?? 0 })}</span>
      )}
    </span>
  );
}
