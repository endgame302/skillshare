// Fork-only (endgame302/skillshare): tests for agentTargets helper.
import { describe, expect, it } from 'vitest';
import type { Target } from '../../../../api/types/targets';
import { agentTargets, codexTargetName } from './agentTargets';

const t = (partial: Partial<Target>) => partial as unknown as Target;

describe('agentTargets', () => {
  it('lists receiving own targets sorted by name and ignores project targets', () => {
    const summary = agentTargets([
      t({ name: 'cursor', agentPath: '~/.cursor/agents' }),
      t({ name: 'claude', agentPath: '~/.claude/agents' }),
      t({ name: 'opencode', agentPath: '' }),
      t({ name: 'proj-claude', project: '/repo', agentPath: '/repo/.claude/agents' }),
    ]);

    expect(summary.receiving).toEqual([
      { name: 'claude', path: '~/.claude/agents' },
      { name: 'cursor', path: '~/.cursor/agents' },
    ]);
  });

  it('reports codex statuses: none, missing, noExtension, ok', () => {
    expect(agentTargets([t({ name: 'claude', agentPath: '~/.claude/agents' })]).codex).toBe('none');
    expect(agentTargets([t({ name: 'codex', project: '/repo' })]).codex).toBe('none');
    expect(agentTargets([t({ name: 'codex' })]).codex).toBe('missing');
    expect(agentTargets([t({ name: 'codex', agentPath: '~/.codex/agents' })]).codex).toBe('noExtension');
    expect(agentTargets([t({ name: 'codex-work', agent: 'codex', agentPath: '~/.codex-work/agents', agentExtension: 'codex-agents' })]).codex).toBe('ok');
  });

  it('detects own gemini targets and ignores project gemini targets', () => {
    expect(agentTargets([t({ name: 'gemini', project: '/repo' })]).gemini).toBe(false);
    expect(agentTargets([t({ name: 'gemini' })]).gemini).toBe(true);
    expect(agentTargets([t({ name: 'gemini-alt', agent: 'gemini' })]).gemini).toBe(true);
  });

  it('returns the own codex target name', () => {
    expect(codexTargetName([t({ name: 'codex-work', agent: 'codex' })])).toBe('codex-work');
    expect(codexTargetName([t({ name: 'codex', project: '/repo' })])).toBeUndefined();
  });
});
