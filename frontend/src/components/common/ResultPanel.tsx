
import type { DomainRecord } from '../../types/domain';
import { EmptyState } from './EmptyState';
import { StatusBadge } from './StatusBadge';

export function ResultPanel({ records }: { records: DomainRecord[] }) {
  if (!records.length) return <EmptyState message="暂无可展示的结果证据" />;
  return <div className="evidence-strip">{records.slice(0, 4).map((item) => {
    const latest = item.revisions?.at(-1);
    const basisRevision = [...(item.revisions || [])].reverse().find((revision) => revision.reviewBasis || revision.runCode);
    const basis = basisRevision?.reviewBasis || item.reviewBasis || '';
    const runCode = basisRevision?.runCode || item.runCode || '';
    const runStatus = basisRevision?.runStatus || item.runStatus || '';
    const runMetricValue = basisRevision?.runMetricValue ?? item.runMetricValue;
    const runMetricUnit = basisRevision?.runMetricUnit || item.runMetricUnit || '';
    const runEvidence = basisRevision?.runEvidence || item.runEvidence || '';
    const runDetail = runCode ? `检测运行 ${runCode} · ${runStatus} · ${runMetricValue ?? '-'} ${runMetricUnit} · 证据：${runEvidence || '无'}` : '';
    return <article key={item.id}>
      <div className="result-title"><strong>{item.code}</strong><StatusBadge status={item.status} /></div>
      <span>{item.name}</span>
      <small title={item.evidence}>{item.evidence || '尚未附加证据'}</small>
      {basis && <small title={basis}>复核依据{basisRevision ? `（v${basisRevision.version}）` : ''}：{basis}</small>}
      {runCode && <small title={runDetail}>检测运行：{runCode} · {runStatus} · {runMetricValue ?? '-'} {runMetricUnit}</small>}
      <small>v{item.version} · {latest?.actor || item.reviewedBy || item.preparedBy || item.owner}</small>
      <code title={latest?.requestId}>{latest?.requestId || '待形成签发请求 ID'}</code>
    </article>;
  })}</div>;
}
