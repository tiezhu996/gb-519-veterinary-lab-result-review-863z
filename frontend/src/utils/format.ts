
export function formatDate(value: string): string {
  return value ? new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) : '-';
}
export function nextStatus(current: string, transitions: Readonly<Record<string, string>>): string | null {
  return transitions[current] || null;
}
export function statusTone(status: string): 'success' | 'warning' | 'danger' | 'neutral' {
  if (/approved|accepted|released|completed|signed|closed|pass|ready|online|cleared|succeeded/.test(status)) return 'success';
  if (/failed|rejected|critical|scrap|discard|revoked|urgent/.test(status)) return 'danger';
  if (/hold|warning|review|pending|restricted|limited|quarantine/.test(status)) return 'warning';
  return 'neutral';
}

const RISK_RANK: Record<string, number> = { low: 1, medium: 2, high: 3, critical: 4 };

/** 高风险结果（high/critical）签发时必须填写复核依据并关联已验证的检测运行。 */
export function isHighRisk(level?: string): boolean {
  return Boolean(level && RISK_RANK[level] >= 3);
}
