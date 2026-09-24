import { useEffect, useState } from 'react';
import TextField from '@mui/material/TextField';
import { UiButton } from './UiButton';
import { RiskTag } from './RiskTag';
import { StatusBadge } from './StatusBadge';
import { isHighRisk } from '../../utils/format';
import type { DomainRecord } from '../../types/domain';

interface ReviewSignoffDialogProps {
  open: boolean;
  item: DomainRecord | null;
  mode: 'sign' | 'reject';
  submitting?: boolean;
  onCancel: () => void;
  onConfirm: (reason: string, reviewBasis: string) => void;
}

/**
 * 复核员对待复核结果作出签发/退回决定的表单。高风险签发必须在表单里填写
 * 书面复核依据；退回补正只需填写退回原因。
 */
export function ReviewSignoffDialog({ open, item, mode, submitting = false, onCancel, onConfirm }: ReviewSignoffDialogProps) {
  const [reason, setReason] = useState('');
  const [basis, setBasis] = useState('');

  useEffect(() => {
    if (open) {
      setReason('');
      setBasis('');
    }
  }, [open, item?.id, mode]);

  if (!open || !item) return null;
  const highRisk = isHighRisk(item.riskLevel);
  const signing = mode === 'sign';
  const basisRequired = signing && highRisk;
  const reasonValid = reason.trim().length >= 3;
  const basisValid = !basisRequired || basis.trim().length >= 5;
  const canSubmit = reasonValid && basisValid && !submitting;

  return <div className="modal-backdrop"><section className="modal review-modal" role="dialog" aria-modal="true">
    <h2>{signing ? '复核签发' : '退回补正'}</h2>
    <div className="review-summary">
      <div><strong>{item.code}</strong><StatusBadge status={item.status} /><RiskTag level={item.riskLevel} /></div>
      <span>{item.name}</span>
      <small>关联编码：{item.relatedCode || '（未填写）'} · 制单人：{item.preparedBy || '-'}</small>
    </div>
    {item.pendingBlockReason && <div className="alert" role="alert">上一次签发被挡下：{item.pendingBlockReason}</div>}
    {signing && highRisk && <div className="review-requirements">
      <strong>高风险签发前置条件（服务端复核）</strong>
      <ul>
        <li>必须填写书面复核依据，不能只凭口头说明；</li>
        <li>按关联编码能找到对应检测运行；</li>
        <li>检测运行已验证通过（validated）；</li>
        <li>检测运行风险等级不低于本签发单。</li>
      </ul>
      <small>缺少任一项时签发会被挡下，记录留在待复核并注明缺项。</small>
    </div>}
    <div className="review-fields">
      <TextField
        label={signing ? '复核决定说明' : '退回补正原因'} required multiline minRows={2}
        value={reason} onChange={(event) => setReason(event.target.value)}
        fullWidth size="small" error={reason.length > 0 && !reasonValid}
        helperText={reason.length > 0 && !reasonValid ? '至少填写 3 个字' : ' '}
      />
      {signing && <TextField
        label={highRisk ? '复核依据（必填）' : '复核依据（低风险可选）'}
        required={basisRequired} multiline minRows={3} value={basis}
        onChange={(event) => setBasis(event.target.value)}
        fullWidth size="small" error={basis.length > 0 && !basisValid}
        placeholder={highRisk ? '请写明判定依据，例如核对的验证报告、质控结果、复检记录编号…' : '可记录复核依据，低风险签发不强制'}
        helperText={basisRequired ? '将与检测运行证据一起写入本次签发版本' : ' '}
      />}
    </div>
    <footer>
      <button className="link-button" onClick={onCancel} disabled={submitting}>取消</button>
      <UiButton danger={!signing} onClick={() => onConfirm(reason.trim(), basis.trim())} disabled={!canSubmit}>
        {submitting ? '提交中…' : signing ? '确认签发' : '确认退回'}
      </UiButton>
    </footer>
  </section></div>;
}
