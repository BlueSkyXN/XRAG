'use strict';
let settingsTab='capabilities',providerState=null,feedbackPage=null,feedbackOffset=0,feedbackTarget=null;
const roleRank={reader:1,contributor:2,maintainer:3,admin:4};
function may(role){return (roleRank[system.identity?.role]||0)>=(roleRank[role]||99)}
function productConnected(){
  $$('[data-admin]').forEach(el=>el.hidden=!may('admin'));
  $$('[data-maintainer]').forEach(el=>el.hidden=!may('maintainer'));
  $('#create-kb-panel').hidden=!may('admin');
  $$('nav [data-page]').forEach(b=>{b.hidden=(['knowledge','feedback'].includes(b.dataset.page)&&!may('contributor'))||(b.dataset.page==='eval'&&!may('maintainer'))});
  for(const id of ['build-btn','upload-btn','estimate-btn']){const b=$('#'+id);b.disabled=!may('maintainer');b.title=may('maintainer')?'':'当前角色只读；此操作需要 Maintainer'}
  $('.source-register').hidden=!may('maintainer');$('#source-list').hidden=!may('maintainer');

  $('#key-kbs').value=kb();
  $('#help-cli').textContent=`./bin/xrag profile add team \\\n  --server ${location.origin==='null'?'http://127.0.0.1:8765':location.origin} \\\n  --key-file /本机/保存的/key --kb ${kb()}\n\n./bin/xrag +ask "日志丢失" --profile team\n./bin/xrag mcp --profile team\n\n./bin/xrag overview --profile team\n./bin/xrag backup list --profile team`;
  if(!may('admin') && ['providers','keys','maintenance'].includes(settingsTab))settingsTab='capabilities';
  if(!may('maintainer') && settingsTab==='data')settingsTab='capabilities';
}
async function loadOverview(){
  const r=await api('/v1/overview'),c=r.counts;
  $('#overview-counts').innerHTML=[['已发布文件',c.files],['资料来源',c.sources],['知识条目',c.knowledge],['来源待处理',c.source_errors]].map(([title,n])=>`<article class="card"><div class="eyebrow">${esc(title)}</div><div class="product-metric">${n}</div></article>`).join('');
  const steps=[['准备资料',c.sources>0?'已登记来源':'添加 MD/TXT 或 Server 目录','files'],['完成构建',c.files>0?'已有可查询文件':'构建成功后再查询','files'],['运行一次明确查询','用错误编号或资料中的关键词','search'],['按需要增加能力',system.embedding?'语义模型已配置':'模型不是首次使用的前提','settings'],['收集反馈、持续改善','处理结果反馈，再用题集比较','feedback']];
  $('#onboarding-steps').innerHTML=steps.map(([t,desc,target])=>`<li><strong>${esc(t)}</strong><p class="subtle">${esc(desc)}</p><button data-open-step="${target}" class="quiet">打开${esc(names[target])} →</button></li>`).join('');
  $$('[data-open-step]').forEach(b=>b.onclick=()=>safe(()=>showPage(b.dataset.openStep)));
  $('#overview-state').innerHTML=`<p><b>${esc(r.version)}</b> · ${esc(system.identity.name)} / ${esc(system.identity.role)}</p><p>内置执行器：<b>${r.workers?'开启':'关闭'}</b>。${r.workers?'处理构建和 Wiki 队列。':'如需执行排队任务，单独运行 worker。'}</p><p>等待 ${r.jobs.queued||0} · 运行 ${r.jobs.running||0} · 失败 ${r.jobs.failed||0}</p><p class="subtle">${r.database_bytes?'当前 SQLite 文件合计 '+sizeText(Object.values(r.database_bytes).reduce((a,b)=>a+b,0)):'只显示当前身份可访问范围的状态。'}</p>`;
}
$('#overview-refresh').onclick=()=>safe(loadOverview);
$$('[data-goto]').forEach(b=>b.onclick=()=>safe(()=>showPage(b.dataset.goto)));
$('#create-kb-form').onsubmit=e=>{e.preventDefault();safe(async()=>{await api('/v1/kbs',{id:$('#new-kb-id').value.trim(),name:$('#new-kb-name').value.trim()});toast('知识库已创建。选择它之后添加资料。');$('#create-kb-form').reset();await connect();await loadOverview()})};
function sizeText(n){return n<1024?n+' B':n<1048576?(n/1024).toFixed(1)+' KiB':(n/1048576).toFixed(1)+' MiB'}
function when(n){return n?new Date(n*1000).toLocaleString():'不过期'}
async function copyText(text){try{await navigator.clipboard.writeText(text);toast('已复制')}catch{toast('当前浏览器未允许自动复制，请选中文字手动复制。')}}
async function downloadResource(path,name){
  const res=await fetch(path,{credentials:'same-origin'});
  if(!res.ok){let v;try{v=await res.json()}catch{};throw Error(v?.error?.message||'下载失败：HTTP '+res.status)}
  const blob=await res.blob(),u=URL.createObjectURL(blob),a=document.createElement('a');a.href=u;a.download=name;document.body.append(a);a.click();a.remove();setTimeout(()=>URL.revokeObjectURL(u),30000);toast('文件已交给浏览器保存')
}
async function loadSettingsPane(name){
  settingsTab=name;$$('.settings-pane').forEach(el=>el.hidden=el.id!=='settings-'+name);$$('[data-settings]').forEach(b=>b.classList.toggle('active',b.dataset.settings===name));
  if(name==='providers' && may('admin'))await loadProviders();
  if(name==='keys' && may('admin'))await loadKeys();
  if(name==='data' && may('admin'))await loadBackups();
}
$$('[data-settings]').forEach(b=>b.onclick=()=>safe(()=>loadSettingsPane(b.dataset.settings)));
async function loadProviders(){
  providerState=await api('/v1/settings/providers');
  const cfg=providerState.editable?providerState.config:providerState.active;
  for(const panel of $$('[data-provider]')){
    const name=panel.dataset.provider,ep=cfg[name];panel.querySelector('[data-enable]').checked=!!ep;
    for(const input of panel.querySelectorAll('[data-field]'))input.value=ep?.[input.dataset.field]??'';
    panel.open=!!ep;panel.querySelector('[data-enable]').disabled=!providerState.editable;
    for(const input of panel.querySelectorAll('[data-field]'))input.disabled=!providerState.editable;
  }
  $('#provider-form button.primary').disabled=!providerState.editable;
  $('#provider-active').textContent=JSON.stringify(providerState.active,null,2);
  const banner=$('#provider-banner');banner.classList.toggle('pending',providerState.restart_required);
  banner.textContent=providerState.source==='external_file'?'当前由启动参数 --config 文件管理。这里只读；切换到内置配置需要去掉 --config 并重启。':providerState.restart_required?'待启用配置与当前进程不同：重启 Server 和所有 Worker 后启用。向量模型改变后请显式重建。':'当前配置与运行配置一致。保存不会探测外部服务，也不会自动调用模型。';
  $('#provider-result').textContent='已保存修订：'+providerState.revision+(providerState.updated?' · '+when(providerState.updated):' · 尚未保存');
}
function providerInput(){const cfg={};for(const panel of $$('[data-provider]')){if(!panel.querySelector('[data-enable]').checked)continue;const ep={};for(const input of panel.querySelectorAll('[data-field]')){const field=input.dataset.field,value=input.value.trim();if(field==='dimension'){ep[field]=Number(value)}else if(value!==''){ep[field]=value}}cfg[panel.dataset.provider]=ep}return cfg}
$('#provider-form').onsubmit=e=>{e.preventDefault();safe(async()=>{const v=await api('/v1/settings/providers',{expected_revision:providerState.revision,config:providerInput()},'PUT');toast('配置已保存。'+(v.restart_required?'重启 Server 和所有 Worker 后启用。':'运行配置未变化。'));await loadProviders()})};
$('#provider-reload').onclick=()=>safe(loadProviders);
$('#provider-export').onclick=()=>downloadJSON(providerInput(),'providers.references.json');
$('#key-role').onchange=()=>{$('#key-kbs').disabled=$('#key-role').value==='admin';$('#key-kbs').required=!$('#key-kbs').disabled};
async function loadKeys(){
  const r=await api('/v1/keys');
  $('#keys-list').innerHTML='<table><thead><tr><th>名称 / 身份</th><th>角色与范围</th><th>有效期</th><th>状态</th><th>操作</th></tr></thead><tbody>'+r.items.map(k=>`<tr><td><b>${esc(k.name)}</b>${k.id===r.current_key_id?'<span class="badge blue">当前</span>':''}<br><small>${esc(k.id)}</small></td><td>${esc(k.role)}<br><small>${esc(k.role==='admin'?'全部知识库':k.kbs.join(', '))}</small></td><td>${esc(when(k.expires_at))}</td><td>${esc(k.state)}</td><td><button data-key-rename="${esc(k.id)}" data-name="${esc(k.name)}">改名</button>${k.disabled?'':` <button data-key-disable="${esc(k.id)}" class="danger">停用</button>`}</td></tr>`).join('')+'</tbody></table>';
  $$('[data-key-rename]').forEach(b=>b.onclick=()=>safe(async()=>{const name=prompt('新的显示名称（不改变身份和已有引用）',b.dataset.name);if(name===null)return;await api('/v1/keys/'+b.dataset.keyRename,{name},'PATCH');await loadKeys()}));
  $$('[data-key-disable]').forEach(b=>b.onclick=()=>safe(async()=>{if(!confirm('停用这把 Key？使用它的应用和会话在下一次请求时将失效。'))return;await api('/v1/keys/'+b.dataset.keyDisable,{confirm:true},'DELETE');toast('Key 已停用');if(b.dataset.keyDisable===r.current_key_id){await logout();return}await loadKeys()}))
}
$('#key-form').onsubmit=e=>{e.preventDefault();safe(async()=>{const expires=$('#key-expires').value;const r=await api('/v1/keys',{name:$('#key-name').value.trim(),role:$('#key-role').value,kbs:$('#key-role').value==='admin'?[]:$('#key-kbs').value.split(',').map(s=>s.trim()).filter(Boolean),expires_at:expires?Math.floor(new Date(expires).getTime()/1000):0});$('#new-key-value').value=r.key;$('#new-key-box').hidden=false;toast('已创建。请先复制保存，新 Key 不会在列表中再次显示。');await loadKeys()})};
$('#copy-new-key').onclick=()=>copyText($('#new-key-value').value);
$('#hide-new-key').onclick=()=>{$('#new-key-value').value='';$('#new-key-box').hidden=true};
function openFeedbackForm(it){
  if(!may('contributor')){toast('提交反馈需要 Contributor 或更高角色。');return}
  feedbackTarget=it;$('#feedback-ref-label').textContent=it.title+' · '+it.path;$('#feedback-note').value='';$('#feedback-label').value='useful';$('#feedback-dialog').showModal()
}
$('#feedback-close').onclick=()=>$('#feedback-dialog').close();
$('#feedback-form').onsubmit=e=>{e.preventDefault();safe(async()=>{if(!feedbackTarget)throw Error('请重新选择结果');await api('/v1/feedback',{kb:feedbackTarget.kb,ref:feedbackTarget.ref,label:$('#feedback-label').value,note:$('#feedback-note').value});$('#feedback-dialog').close();feedbackTarget=null;toast('反馈已保存，可在“反馈处理”查看。')})};
async function loadFeedback(){
  feedbackPage=await api('/v1/feedback?kb='+encodeURIComponent(kb())+'&limit=20&offset='+feedbackOffset+'&status='+$('#feedback-status').value);
  const labels={useful:'有用',irrelevant:'不相关',wrong:'内容有误',outdated:'已过时'},states={open:'待处理',resolved:'已处理',dismissed:'不采纳'};
  $('#feedback-count').textContent='当前条件共 '+feedbackPage.total+' 条，本页 '+feedbackPage.items.length+' 条';
  $('#feedback-list').innerHTML=feedbackPage.items.map((f,i)=>`<article class="result-card"><div class="result-top"><b>${esc(labels[f.label]||f.label)}</b><span class="badge ${f.status==='resolved'?'green':'blue'}">${esc(states[f.status])}</span></div><p class="subtle">${esc(f.id)} · ${when(f.created)}</p><p class="path">${esc(f.path||'来源已撤回 / 不可用')}</p><p class="feedback-note">${esc(f.note||'未填写说明')}</p>${f.ref?`<button data-feedback-read="${i}">读取所引用版本</button>`:'<p class="subtle">不返回已撤回来源及相关说明。</p>'}${may('maintainer')?`<form class="resolution-grid" data-resolution="${i}"><label>处理结果<select name="status">${Object.entries(states).map(([v,t])=>`<option value="${v}" ${f.status===v?'selected':''}>${t}</option>`).join('')}</select></label><label>处理说明<input name="resolution" maxlength="8000" value="${esc(f.resolution)}" placeholder="说明已做的调整或不采纳原因"></label><button class="primary">保存处理</button></form>`:`<p class="subtle">处理说明：${esc(f.resolution||'暂无')}</p>`}</article>`).join('')||'<div class="empty">当前没有反馈。从检索结果的“反馈”按钮开始。</div>';
  $$('[data-feedback-read]').forEach(b=>b.onclick=()=>safe(()=>read(feedbackPage.items[Number(b.dataset.feedbackRead)].ref)));
  $$('[data-resolution]').forEach(f=>f.onsubmit=e=>{e.preventDefault();safe(async()=>{const old=feedbackPage.items[Number(f.dataset.resolution)];await api('/v1/feedback/'+old.id,{status:f.elements.status.value,resolution:f.elements.resolution.value,expected_revision:old.revision},'PATCH');toast('处理结果已保存');await loadFeedback()})});
  $('#feedback-pagination').innerHTML=(feedbackOffset?'<button id="feedback-prev">上一页</button>':'')+(feedbackPage.next_offset!==null?'<button id="feedback-next">下一页</button>':'');
  if($('#feedback-prev'))$('#feedback-prev').onclick=()=>safe(async()=>{feedbackOffset=Math.max(0,feedbackOffset-20);await loadFeedback()});
  if($('#feedback-next'))$('#feedback-next').onclick=()=>safe(async()=>{feedbackOffset=feedbackPage.next_offset;await loadFeedback()});
}
$('#feedback-refresh').onclick=()=>safe(loadFeedback);
$('#feedback-status').onchange=()=>{feedbackOffset=0;safe(loadFeedback)};
$('#feedback-export').onclick=()=>{if(feedbackPage)downloadJSON({...feedbackPage,offset:feedbackOffset,kb:kb(),export_scope:'current displayed page'},'feedback-page.json')};
$('#export-kb').onclick=()=>safe(()=>downloadResource('/v1/kbs/'+encodeURIComponent(kb())+'/export','xrag-'+kb()+'-published.zip'));
async function loadBackups(){
  const r=await api('/v1/backups');$('#backup-list').innerHTML='<h2>已保存快照</h2>'+r.items.map(b=>`<article class="card"><div class="result-top"><b>${esc(b.id)}</b><span class="badge">${sizeText(b.size)}</span></div><p class="subtle">${when(b.created)} · ${b.available?'文件可用':'文件不在原位置'}</p><p class="subtle">SHA-256 <code>${esc(b.sha256)}</code></p><div class="backup-actions"><button data-backup-download="${esc(b.id)}" ${b.available?'':'disabled'} class="primary">下载快照</button><button data-backup-remove="${esc(b.id)}" class="danger">删除这份备份</button></div></article>`).join('');if(!r.items.length)$('#backup-list').innerHTML='<div class="empty">尚未创建数据库快照。</div>';
  $$('[data-backup-download]').forEach(b=>b.onclick=()=>safe(()=>downloadResource('/v1/backups/'+b.dataset.backupDownload+'/download',b.dataset.backupDownload+'.sqlite')));
  $$('[data-backup-remove]').forEach(b=>b.onclick=()=>safe(async()=>{if(!confirm('只删除这份备份，不影响正在运行的资料库。继续？'))return;await api('/v1/backups/'+b.dataset.backupRemove,{confirm:true},'DELETE');await loadBackups()}));
}
$('#backup-create').onclick=()=>safe(async()=>{if(!confirm('创建 SQLite 快照？它包含入库内容和草稿，但不包含外部来源目录与实际 Key。'))return;const b=$('#backup-create');b.disabled=true;try{const r=await api('/v1/backups',{confirm:true});toast('快照已完成：'+r.id);await loadBackups()}finally{b.disabled=false}});
$('#cache-preview').onclick=()=>safe(async()=>{$('#cache-result').textContent=JSON.stringify(await api('/v1/maintenance/cache'),null,2)});
$('#cache-clean').onclick=()=>safe(async()=>{if(!confirm('清理已经过期的结果页、范围句柄和查询摘要？不会清理正文或历史版本。'))return;$('#cache-result').textContent=JSON.stringify(await api('/v1/maintenance/cache',{confirm:true}),null,2)});
async function logout(){
  if(typeof stopResearchStream9==='function')stopResearchStream9('未订阅');
  if(typeof selectedResearch!=='undefined')selectedResearch='';
  if($('#research-result'))$('#research-result').textContent='';if($('#research-list'))$('#research-list').textContent='';
  await api('/v1/session',{},'DELETE');
  for(const id of ['results','graph-result','file-list','knowledge-list','job-list','eval-report','query-list','query-detail','keys-list','backup-list','feedback-list','reader-body','reader-json','trace','search-summary','pagination','provider-active','overview-state','overview-counts','source-list','eval-runs','system-json','maintenance-runs','maintenance-preview-result','eval-drafts','diagnose-result'])if($('#'+id))$('#'+id).textContent='';
  for(const f of $$('form'))f.reset();$('#new-key-value').value='';$('#new-key-box').hidden=true;$('#feedback-dialog').close();$('#reader').close();
  lastResult=null;fileItems=[];knowledgeItems=[];editingKnowledge=null;feedbackPage=null;feedbackTarget=null;providerState=null;system={};scope='';settingsTab='capabilities';pageName='search';feedbackOffset=0;
  $('#app').hidden=true;$('#login').hidden=false;prepareLogin().then(()=>{if(!$('#password-form').hidden)$('#user-input').focus();else $('#key-input').focus()});toast('已退出。');
}
$('#logout-btn').onclick=()=>safe(logout);
if(system.identity)productConnected();
