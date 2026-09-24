import { useEffect, useMemo, useState } from 'react';
import type { EntityConfig, DomainRecord } from '../types/domain';
import type { EntityStore } from '../stores/factory';
import { nextStatus, formatDate, isHighRisk } from '../utils/format';
import { StatusBadge } from './common/StatusBadge';
import { RiskTag } from './common/RiskTag';
import { ResultPanel } from './common/ResultPanel';
import { EmptyState } from './common/EmptyState';
import { MetricCard } from './common/MetricCard';
import { ConfirmDialog } from './common/ConfirmDialog';
import { ReviewSignoffDialog } from './common/ReviewSignoffDialog';
import { UiButton } from './common/UiButton';
import { useAuth } from '../hooks/useAuth';

interface EntityPageProps {
  config: EntityConfig;
  useStore: EntityStore;
  showRiskTags?: boolean;
  showResultPanel?: boolean;
}

interface ReviewPrompt { item: DomainRecord; mode: 'sign' | 'reject' }

export function EntityPage({ config, useStore, showRiskTags = false, showResultPanel = false }: EntityPageProps) {
  const { items, meta, loading, error, load, createRecord, transition } = useStore();
  const { session, hasRole } = useAuth();
  const [search, setSearch] = useState('');
  const [showCreate, setShowCreate] = useState(false);
  const [pending, setPending] = useState<{ item: DomainRecord; status: string } | null>(null);
  const [review, setReview] = useState<ReviewPrompt | null>(null);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => { void load(config.path); }, [config.path, load]);
  const highRisk = useMemo(() => items.filter((item) => isHighRisk(item.riskLevel)).length, [items]);
  const canOperate = hasRole('operator');
  const isSignoff = config.key === 'resultSignoff';

  const createDemo = async () => {
    const now = Date.now();
    await createRecord(config.path, {
      code: `${config.key.toUpperCase()}-${now.toString().slice(-6)}`,
      name: `新增${config.label}`,
      description: '通过前端工作台创建的业务记录',
      facility: '默认检验区', owner: session?.displayName || '现场操作员', category: '常规', riskLevel: 'medium',
      metricValue: 25, metricUnit: 'unit', effectiveAt: new Date().toISOString(), evidence: '已完成创建前证据核对', relatedCode: '',
    });
    setSearch('');
    setShowCreate(false);
  };

  const requiresPreparer = (item: DomainRecord) => isSignoff && item.status === 'draft';
  const requiresReviewer = (item: DomainRecord) => isSignoff && item.status === 'peer_review';
  const isOriginalPreparer = (item: DomainRecord) => Boolean(session?.username && session.username === item.preparedBy);
  const canAdvance = (item: DomainRecord) => canOperate
    && (!requiresPreparer(item) || isOriginalPreparer(item))
    && (!requiresReviewer(item) || (hasRole('reviewer') && !isOriginalPreparer(item)));

  const unavailableReason = (item: DomainRecord) => {
    if (!canOperate) return '只读';
    if (requiresPreparer(item) && !isOriginalPreparer(item)) return '等待制单人';
    if (requiresReviewer(item) && !hasRole('reviewer')) return '等待复核员';
    if (requiresReviewer(item) && isOriginalPreparer(item)) return '需异人复核';
    return '流程结束';
  };

  const confirmTransition = async () => {
    if (!pending) return;
    await transition(config.path, pending.item, pending.status);
    setSearch('');
    setPending(null);
  };

  const confirmReview = async (reason: string, reviewBasis: string) => {
    if (!review) return;
    const target = review.mode === 'sign' ? 'signed' : 'rejected';
    setSubmitting(true);
    try {
      await transition(config.path, review.item, target, { reason, reviewBasis });
      setSearch('');
      setReview(null);
    } catch {
      // store 已把服务端返回的缺项说明写入错误条，保留对话框方便补正
    } finally {
      setSubmitting(false);
    }
  };

  const renderActions = (item: DomainRecord) => {
    const next = nextStatus(item.status, config.primaryTransitions);
    if (requiresReviewer(item)) {
      if (!canAdvance(item)) return <span className="muted">{unavailableReason(item)}</span>;
      return <span className="row-actions">
        <button className="table-action" onClick={() => setReview({ item, mode: 'sign' })}>
          复核签发{isHighRisk(item.riskLevel) ? '（高风险）' : ''}
        </button>
        <button className="table-action table-action--ghost" onClick={() => setReview({ item, mode: 'reject' })}>退回补正</button>
      </span>;
    }
    if (next && canAdvance(item)) {
      return <button className="table-action" onClick={() => setPending({ item, status: next })}>推进至 {next}</button>;
    }
    return <span className="muted">{next ? unavailableReason(item) : '流程结束'}</span>;
  };

  return <main className="workspace">
    <header className="page-header"><div><p className="eyebrow">业务工作台</p><h1>{config.label}</h1><p>统一管理{config.label}的状态、风险、证据与责任人。</p></div>{canOperate ? <UiButton onClick={() => setShowCreate(true)}>新增{config.label}</UiButton> : <span className="access-note">只读权限</span>}</header>
    <section className="metrics"><MetricCard label="记录总数" value={meta.total} detail="当前筛选范围"/><MetricCard label="高风险" value={highRisk} detail="需要书面复核依据"/><MetricCard label="状态种类" value={new Set(items.map((item) => item.status)).size} detail="状态机覆盖"/></section>
    {showResultPanel && <section className="result-section"><header><h2>结果与版本证据</h2><span>签发版本、复核依据、检测运行证据、操作者和请求 ID 可追溯</span></header><ResultPanel records={items} /></section>}
    <section className="toolbar"><input aria-label="搜索" placeholder={`搜索${config.label}编码或名称`} value={search} onChange={(event) => setSearch(event.target.value)} /><UiButton onClick={() => void load(config.path, search)}>查询</UiButton><button className="link-button" onClick={() => { setSearch(''); void load(config.path); }}>重置</button></section>
    {error && <div className="alert" role="alert">{error}</div>}
    <section className="table-shell" aria-busy={loading}><table><thead><tr><th>编码</th><th>名称</th><th>状态</th><th>风险</th><th>责任人</th><th>指标</th><th>更新时间</th><th>操作</th></tr></thead><tbody>
      {items.map((item) => { return <tr key={item.id}><td><strong>{item.code}</strong>{item.pendingBlockReason && <small className="block-note" title={item.pendingBlockReason}>⚠ 待补：{item.pendingBlockReason}</small>}</td><td>{item.name}<small>{item.facility}</small></td><td><StatusBadge status={item.status}/></td><td>{showRiskTags ? <RiskTag level={item.riskLevel}/> : item.riskLevel}</td><td>{item.owner}</td><td>{item.metricValue} {item.metricUnit}</td><td>{formatDate(item.updatedAt)}</td><td>{renderActions(item)}</td></tr>; })}
      {!items.length && !loading && <EmptyState message="暂无记录" colSpan={8} />}
    </tbody></table>{loading && <div className="loading">正在同步业务数据…</div>}</section>
    <ConfirmDialog open={showCreate} title={`新增${config.label}`} onCancel={() => setShowCreate(false)} onConfirm={() => void createDemo().catch(() => undefined)}><p>将创建一条包含完整责任人、风险和证据信息的演示记录。</p></ConfirmDialog>
    <ConfirmDialog open={Boolean(pending)} title="确认状态迁移" onCancel={() => setPending(null)} onConfirm={() => void confirmTransition().catch(() => undefined)}><p>状态迁移会写入不可覆盖的版本与审计日志。</p><strong>{pending?.item.status} → {pending?.status}</strong></ConfirmDialog>
    <ReviewSignoffDialog
      open={Boolean(review)}
      item={review ? items.find((candidate) => candidate.id === review.item.id) || review.item : null}
      mode={review?.mode || 'sign'}
      submitting={submitting} onCancel={() => setReview(null)}
      onConfirm={(reason, basis) => void confirmReview(reason, basis)}
    />
  </main>;
}
