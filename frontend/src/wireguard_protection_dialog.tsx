import { useState } from "react";
import { Button, ErrorLine, Field, Modal } from "./ui";
import { errorMessage, type Dict } from "./api";
import type { Act } from "./nodes";

export function WireGuardProtectionDialog({ rows, act, onSaved, onClose }: {
 rows:Dict[]; act:Act; onSaved:()=>Promise<void>; onClose:()=>void;
}) {
 const [enabled,setEnabled]=useState(rows.some(row=>row.protected===true));
 const [busy,setBusy]=useState(false),[failure,setFailure]=useState("");
 const changed=rows.some(row=>(row.protected===true)!==enabled);
 async function save(){
  setBusy(true);setFailure("");
  try {
   const ok=await act("/api/network/wireguard/protection",{interfaces:rows.map(row=>row.name),protected:enabled});
   if(!ok){setFailure("接口保护未保存，请检查操作提示后重试");return;}
   await onSaved();onClose();
  } catch(error){setFailure(errorMessage(error));}finally{setBusy(false);}
 }
 return <Modal title={rows.length>1?"批量设置接口保护":"接口保护"} onClose={()=>{if(!busy)onClose();}}>
  <p>所选接口：{rows.map(row=>String(row.name)+"（"+(row.protected===true?"已开启":"已关闭")+"）").join("、")}</p>
  <Field label="接口保护" info={<ul>
   <li><strong>开启：</strong>接口启停、配置变更和相关受保护路由变更需要额外确认。</li>
   <li><strong>关闭：</strong>取消该接口的额外保护确认；常规参数校验、运行中配置变更确认和 VPN 模块归属限制仍保留。</li>
   <li><strong>保存：</strong>只修改 FoXray 元数据，不启停接口，不改地址、Peer、路由、SNAT 或开机自启。重启管理服务后继续保留。</li>
  </ul>}><span className="check"><input aria-label="启用接口保护" type="checkbox" checked={enabled} disabled={busy} onChange={e=>setEnabled(e.target.checked)}/>{enabled?"已开启":"已关闭"}</span></Field>
  <ErrorLine error={failure}/>
  <div className="modal-actions"><Button disabled={busy} onClick={onClose}>取消</Button><Button variant="primary" disabled={busy||!changed} onClick={()=>void save()}>{busy?"正在保存…":"保存"}</Button></div>
 </Modal>;
}
