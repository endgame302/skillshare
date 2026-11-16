// Fork-only (endgame302/skillshare): ClawHub strings chosen at run time.
import { ApiError } from '../../../../api/client';
import type { ClawHubBlocked } from '../../../../api/forkClawhub';

type T = (key: string, params?: Record<string, string | number>) => string;

export const blockedKey: Record<ClawHubBlocked, string> = {
  unavailable: 'clawhub.blocked.unavailable',
  no_integrity: 'clawhub.blocked.noIntegrity',
  unsupported: 'clawhub.blocked.unsupported',
};

/** A trust level ClawHub names, in words; an unknown one as ClawHub spells it. */
export function trustLabel(t: T, trust: string): string {
  return trust === 'official' ? t('clawhub.trust.official') : trust;
}

/** What went wrong with an install or the feed, in the reader's language where the server said why. */
export function clawhubErrorText(t: T, err: unknown): string {
  if (!(err instanceof ApiError)) return (err as Error)?.message ?? String(err);
  const detail = err.message;
  const params = (err.params ?? {}) as Record<string, string>;
  switch (err.code) {
    case 'clawhub_integrity_mismatch':
      return t('clawhub.error.integrity');
    case 'clawhub_unsafe_archive':
      return t('clawhub.error.unsafe', { detail });
    case 'clawhub_no_skill_file':
      return t('clawhub.error.noSkill');
    case 'clawhub_version_changed':
      return t('clawhub.error.versionChanged', { version: params.version ?? '' });
    case 'clawhub_not_found':
      return t('clawhub.error.notFound');
    case 'clawhub_download_failed':
      return t('clawhub.error.download', { detail });
    case 'clawhub_feed_unavailable':
      return t('clawhub.error.feed', { detail });
    case 'clawhub_not_installable': {
      const key = blockedKey[params.reason as ClawHubBlocked];
      return key ? t(key) : detail;
    }
    default:
      return detail;
  }
}
