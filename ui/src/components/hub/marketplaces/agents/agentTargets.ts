// Fork-only (endgame302/skillshare): summarize which user targets receive installed agents.
import type { Target } from '../../../../api/types/targets';

export type CodexAgentStatus = 'none' | 'missing' | 'noExtension' | 'ok';

export interface AgentTargetsSummary {
  receiving: { name: string; path: string }[];
  codex: CodexAgentStatus;
  gemini: boolean;
}

export function agentTargets(targets: Target[]): AgentTargetsSummary {
  const own = targets.filter((t) => !t.project);

  const receiving = own
    .filter((t) => Boolean(t.agentPath))
    .map((t) => ({ name: t.name, path: t.agentPath! }))
    .sort((a, b) => a.name.localeCompare(b.name));

  const codexTarget = own.find((t) => (t.agent ?? t.name) === 'codex');
  let codex: CodexAgentStatus = 'none';
  if (codexTarget) {
    if (!codexTarget.agentPath) {
      codex = 'missing';
    } else if (codexTarget.agentExtension !== 'codex-agents') {
      codex = 'noExtension';
    } else {
      codex = 'ok';
    }
  }

  const gemini = own.some((t) => (t.agent ?? t.name) === 'gemini');

  return { receiving, codex, gemini };
}

export function codexTargetName(targets: Target[]): string | undefined {
  return targets.find((t) => !t.project && (t.agent ?? t.name) === 'codex')?.name;
}
