'use strict';
let selectedResearch='', researchStream9=null, researchStreamTask9='';
const defaultRecipe8={version:'xrag.recipe/v1',name:'keyword-then-read',steps:[{action:'search.keyword',arguments:{q:'$input',limit:3}},{action:'files.read',arguments:{ref:'$last.ref',length:3000}}]};
$('#recipe-json').value=JSON.stringify(defaultRecipe8,null,2);
$('#research-mode').onchange=()=>{$('#recipe-options').open=$('#research-mode').value==='recipe'};
$('#research-form').onsubmit=e=>{e.preventDefault();safe(async()=>{const type=$('#research-mode').value;const payload={kb:kb(),goal:$('#research-goal').value,path:$('#research-path').value,max_steps:Number($('#research-steps').value),max_seconds:Number($('#research-seconds').value)};if(type==='recipe')payload.recipe=JSON.parse($('#recipe-json').value);const j=await api(type==='recipe'?'/v1/recipes/run':'/v1/research',payload);selectedResearch=j.id;toast('任务已入队：'+j.id);await loadResearch()})};
async function loadResearch(){const rs=await api('/v1/jobs');const jobs=rs.items.filter(j=>['research','recipe'].includes(j.kind)&&j.kb===kb());$('#research-list').innerHTML=jobs.length?jobs.map(j=>`<article class="result-card"><div class="result-top"><b>${esc(j.kind==='research'?'委托调查':'固定配方')} · ${esc(j.payload.goal)}</b><span class="badge ${j.state==='succeeded'?'green':'blue'}">${esc(j.state)}</span></div><p class="subtle">${esc(j.id)}</p><button data-task-view="${esc(j.id)}">查看过程与依据</button>${['queued','running'].includes(j.state)?` <button data-task-cancel="${esc(j.id)}">取消任务</button>`:''}</article>`).join(''):'<div class="empty">尚未提交调查。可以先选择固定配方，用现有资料试用。</div>';$$('[data-task-view]').forEach(b=>b.onclick=()=>safe(async()=>{stopResearchStream9();selectedResearch=b.dataset.taskView;await showResearch8(selectedResearch)}));$$('[data-task-cancel]').forEach(b=>b.onclick=()=>safe(async()=>{await api('/v1/jobs/'+b.dataset.taskCancel+'/cancel',{});await loadResearch()}));if(selectedResearch&&jobs.some(x=>x.id===selectedResearch)&&researchStreamTask9!==selectedResearch)await showResearch8(selectedResearch)}
async function showResearch8(id){const j=await api('/v1/jobs/'+id);renderResearch9(j)}
function renderResearch9(j){const r=j.result||{};$('#research-result').innerHTML=`<article class="card"><div class="result-top"><h2>${esc(j.payload.goal||'调查结果')}</h2><span class="badge">${esc(j.state)} · ${esc(r.stop_reason||'等待执行')}</span></div>${j.error?`<p class="job-error">${esc(j.error)}</p>`:''}${r.checkpoint?`<p class="subtle">检查点 ${esc(r.checkpoint.revision)} · ${esc(r.checkpoint.phase)} · ${r.checkpoint.phase==='complete'?'已完成':'下一步 '+(Number(r.checkpoint.next_step)+1)}${r.checkpoint.blocking_reason?' · '+esc(r.checkpoint.blocking_reason):''}</p>`:''}${j.state==='failed'&&!j.cancel_requested?'<button id="research-resume-selected" class="primary">检查条件并继续原任务</button>':''}${r.unavailable?`<p>${esc(r.message)}</p>`:''}${(r.findings||[]).map(f=>`<div class="result-card"><p>${esc(f.claim)}</p><small>依据 ${esc(f.evidence_ids.join('、'))}</small></div>`).join('')}<p class="subtle">${esc((r.unresolved||[]).join('；'))}</p><h3>类型化依据</h3>${(r.evidence||[]).map((e,i)=>`<details class="card"><summary>${esc(e.id)} · ${esc(e.kind)}${e.metric?` · ${esc(e.metric.value)} ${esc(e.metric.unit)}`:''}</summary><pre>${esc(JSON.stringify(e.content,null,2))}</pre><button data-task-evidence="${i}">读取依据</button></details>`).join('')}<details><summary>实际步骤 / 原始结果</summary><pre>${esc(JSON.stringify(r,null,2))}</pre></details><p class="subtle">${r.references_expire_at?'结果引用保留至 '+esc(new Date(r.references_expire_at*1000).toLocaleString())+'；资料撤回仍然优先。':'未声明长期引用保留。'}</p></article>`;if($('#research-resume-selected'))$('#research-resume-selected').onclick=()=>safe(async()=>{await api('/v1/tasks/resume',{id:j.id});toast('原任务已重新入队；预算与截止时间保持不变');await loadResearch()});$$('[data-task-evidence]').forEach(b=>b.onclick=()=>safe(async()=>{const e=r.evidence[Number(b.dataset.taskEvidence)];const response=await action('files.read',{ref:e.ref});$('#reader-title').textContent=e.id+' · '+e.kind;$('#reader-meta').textContent='原任务依据';$('#reader-body').textContent=response.data?.text||JSON.stringify(response.data,null,2);$('#reader-json').textContent=JSON.stringify(response,null,2);$('#reader-next').innerHTML='';$('#reader').showModal()}))}
$('#research-refresh').onclick=()=>safe(loadResearch);
setInterval(()=>{if(pageName==='research'&&!document.hidden&&!$('#app').hidden)safe(loadResearch)},2500);

function stopResearchStream9(message='已停止查看；未取消任务'){
 if(researchStream9)researchStream9.close();researchStream9=null;researchStreamTask9='';
 $('#research-stream-status').textContent=message;
}
function startResearchStream9(){
 if(!selectedResearch)throw Error('先选择一个调查或配方任务');
 stopResearchStream9('正在连接…');
 const id=selectedResearch;researchStreamTask9=id;
 const stream=new EventSource('/v1/tasks/events?id='+encodeURIComponent(id));researchStream9=stream;
 stream.addEventListener('task',e=>{if(researchStream9!==stream)return;try{
  const v=JSON.parse(e.data);if(v.job.id!==id)return;
  renderResearch9(v.job);$('#research-stream-status').textContent='实时状态：'+v.job.state+' · '+id;
 }catch{stopResearchStream9('事件内容无效，请刷新任务')}});
 stream.addEventListener('done',e=>{if(researchStream9!==stream)return;stopResearchStream9('任务已结束；可重新查看保存结果')});
 stream.addEventListener('reconnect',()=>{if(researchStream9===stream)stopResearchStream9('订阅到期；点击重新查看当前状态')});
 stream.addEventListener('error',e=>{if(researchStream9!==stream)return;let message='连接已断开；任务不会被取消，可以重新订阅';if(e.data){try{const v=JSON.parse(e.data);message=(v.error?.code||'错误')+' · '+(v.error?.message||message)}catch{}}
 stopResearchStream9(message)});
}
$('#research-stream-start').onclick=()=>safe(startResearchStream9);
$('#research-stream-stop').onclick=()=>stopResearchStream9();
window.addEventListener('beforeunload',()=>{if(researchStream9)researchStream9.close()});
document.addEventListener('visibilitychange',()=>{if(document.hidden&&researchStream9)stopResearchStream9('页面隐藏，已暂停订阅；任务继续执行')});
setInterval(()=>{if(researchStream9&&(pageName!=='research'||$('#app').hidden))stopResearchStream9()},500);
