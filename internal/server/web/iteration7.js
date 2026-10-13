'use strict';
const sourceLoad7=loadSources;
loadSources=async function(){
 await sourceLoad7();
 $$('#source-list .source-card').forEach((card,i)=>{
  const {source:s,policy:p}=sourcesData[i];if(s.kind!=='git')return;
  const button=document.createElement('button');button.className='quiet';button.textContent='Git 推送通知';button.dataset.nativeHook=s.id;
  (card.querySelector('.inline-actions')||card).append(button);
  button.onclick=()=>safe(async()=>{
   openLife('Git 推送通知：'+s.id,`<form id="native-hook-form"><label>协议<select id="hook-provider"><option value="custom">XRAG v1</option><option value="github">GitHub Push</option><option value="gitlab">GitLab Push / Tag Push</option><option value="gitea">Gitea Push</option></select></label><label>仓库路径<input id="hook-repository" value="${esc(p.hook_repository||'')}" placeholder="group/project"></label><label>选定 ref<input id="hook-ref" value="${esc(p.hook_ref||'')}" placeholder="refs/heads/main"></label><label>密钥引用<input id="hook-secret" value="${esc(p.hook_secret_ref||'')}" placeholder="env:XRAG_GIT_HOOK"></label><p class="subtle">通知只安排重新扫描已登记的来源，不读取请求中的仓库地址，不执行提交里的命令。Server 与 Worker 的 Git 连接仍需单独可用。</p><p>接收地址：<code>/v1/hooks/${esc(kb())}/${esc(s.id)}</code>。仅处理 JSON 格式；其他事件不会构建。</p><button class="primary">保存通知设置</button></form>`);
   $('#hook-provider').value=p.hook_provider||'custom';
   $('#native-hook-form').onsubmit=event=>{event.preventDefault();safe(async()=>{
    const provider=$('#hook-provider').value;
    await api('/v1/kbs/'+kb()+'/sources/'+s.id+'/policy',{...p,hook_provider:provider,hook_repository:provider==='custom'?'':$('#hook-repository').value.trim(),hook_ref:provider==='custom'?'':$('#hook-ref').value.trim(),hook_secret_ref:$('#hook-secret').value.trim()},'PUT');
    lifeDialog.close();await loadSources();toast('通知协议已保存。保存不会扫描仓库或启动构建。');
   })};
  });
 });
};
// Collection deletion remains an offline command, not a hidden background task.
const dataPane7=$('#settings-data');
if(dataPane7){const card=document.createElement('article');card.className='card';card.dataset.admin='';card.innerHTML='<h2>退休向量集合回收</h2><p>先完成文件版清理，再停止当前资料域的 Server 和 Worker。默认命令只读本地记录；确认执行时还会检查上游集合身份、别名和实际点数。</p><pre>./bin/xrag-server vector-gc --data /资料目录 --keep-days 30\n# 确认后才增加 --confirm</pre><p class="subtle">仍有内容、历史依据持有、当前配置选中或任何别名指向的集合不删除。它不是在线 GC，也不删除任意外部向量集合。</p>';dataPane7.append(card)}
