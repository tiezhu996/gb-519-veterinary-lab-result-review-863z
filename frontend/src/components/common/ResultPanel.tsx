
import { useState } from 'react';
import type { DomainRecord, SignoffRevision } from '../../types/domain';
import { EmptyState } from './EmptyState';
import { StatusBadge } from './StatusBadge';
import { formatDate } from '../../utils/format';

export function ResultPanel({ records }: { records: DomainRecord[] }) {
  const [expanded, setExpanded] = useState<number | null>(null);
  if (!records.length) return <EmptyState message="暂无可展示的结果证据" />;
  return <div className="evidence-strip">{records.slice(0, 4).map((item) => {
    const latest = item.revisions?.at(-1);
    const open = expanded === item.id;
    return <article key={item.id}>
      <div className="result-title"><strong>{item.code}</strong><StatusBadge status={item.status} /></div>
      <span>{item.name}</span>
      <small title={item.evidence}>{item.evidence || '尚未附加证据'}</small>
      {item.reviewBasis && <small className="basis-line" title={item.reviewBasis}>复核依据：{item.reviewBasis}</small>}
      {item.assayEvidence && <small className="assay-line" title={item.assayEvidence.assayEvidence}>
        检测运行 {item.assayEvidence.assayCode} · {item.assayEvidence.assayStatus} · {item.assayEvidence.assayMetricName} {item.assayEvidence.assayMetricValue}{item.assayEvidence.assayMetricUnit}
      </small>}
      <small>v{item.version} · {latest?.actor || item.reviewedBy || item.preparedBy || item.owner}</small>
      <code title={latest?.requestId}>{latest?.requestId || '待形成签发请求 ID'}</code>
      {item.revisions && item.revisions.length > 0 && <button className="link-button revision-toggle" onClick={() => setExpanded(open ? null : item.id)}>
        {open ? '收起历史版本' : `查看历史版本（${item.revisions.length}）`}
      </button>}
      {open && <ul className="revision-list">{item.revisions!.map((revision) => <RevisionLine key={revision.id} revision={revision} />)}</ul>}
    </article>;
  })}</div>;
}

function RevisionLine({ revision }: { revision: SignoffRevision }) {
  return <li>
    <header>
      <strong>v{revision.version}</strong>
      <StatusBadge status={revision.status} />
      <span>{revision.actor}</span>
      <time>{formatDate(revision.createdAt)}</time>
    </header>
    <p title={revision.evidence}>{revision.evidence || '（无证据）'}</p>
    {revision.reviewBasis && <p className="basis-line">复核依据：{revision.reviewBasis}</p>}
    {revision.assayCode && <p className="assay-line">
      检测运行 {revision.assayCode} · {revision.assayStatus} · {revision.assayMetricName} {revision.assayMetricValue}{revision.assayMetricUnit}
      {revision.assayEvidence && <em>{revision.assayEvidence}</em>}
    </p>}
    <small>{revision.reason || revision.action} · 请求 {revision.requestId}</small>
  </li>;
}
