package main

const panelTemplates = `
{{define "head"}}<!DOCTYPE html><html lang="{{.Lang}}"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>{{.T.docTitle}}</title>
<style>
body{font:16px/1.5 system-ui,sans-serif;margin:0;background:#f4f5f7;color:#1c2024}
main{max-width:860px;margin:0 auto;padding:16px}
h1{font-size:22px;margin:8px 0 16px} h2{font-size:17px;margin:24px 0 8px}
.bar{display:flex;gap:8px;flex-wrap:wrap;margin-bottom:12px}
a.btn,button{display:inline-block;padding:9px 14px;border:1px solid #c5cad1;border-radius:8px;background:#fff;color:#1c2024;font:inherit;text-decoration:none;cursor:pointer}
.pri{background:#1f6feb!important;border-color:#1f6feb!important;color:#fff!important}
.msg{background:#e7f5ec;border:1px solid #9fd5b1;padding:10px 14px;border-radius:8px;margin-bottom:12px}
table{width:100%;border-collapse:collapse;background:#fff;border-radius:8px;overflow:hidden}
td,th{padding:9px 12px;border-bottom:1px solid #eceef1;text-align:left;vertical-align:middle}
td form{display:inline} .sm{font-size:14px;padding:5px 10px}
input[type=text],input[type=date],input[type=password],input[type=number]{width:100%;box-sizing:border-box;padding:9px;border:1px solid #c5cad1;border-radius:8px;font:inherit}
label{display:block;margin:12px 0 4px;font-weight:600}
#ed{min-height:260px;background:#fff;border:1px solid #c5cad1;border-radius:8px;padding:12px}
#ed img{max-width:100%;height:auto}
.tb{display:flex;gap:6px;flex-wrap:wrap;margin-bottom:6px}
.hint{color:#5b6570;font-size:14px}.err{background:#fdecea;border:1px solid #f0a8a0;padding:10px 14px;border-radius:8px;margin-bottom:12px}
pre{background:#fff;padding:14px;border-radius:8px;white-space:pre-wrap}
.lang{text-align:right;font-size:14px;margin:0 0 4px;color:#5b6570}
.lang a{color:#1f6feb}
</style></head><body><main>
<div class="lang">{{if eq .Lang "pl"}}<a href="/lang?l=en">EN</a> | <b>PL</b>{{else}}<b>EN</b> | <a href="/lang?l=pl">PL</a>{{end}}</div>
{{end}}

{{define "index"}}{{template "head" .}}
<h1>{{.T.panelTitle}}</h1>
<p class="hint">{{.T.project}} <b>{{.ProjectName}}</b> ({{.Dir}}) | <a href="/start">{{.T.switchProject}}</a></p>
{{if .Msg}}<div class="msg">{{.Msg}}</div>{{end}}
<div class="bar">
 <a class="btn pri" href="/edit?kind=post">{{.T.newPost}}</a>
 <form method="post" action="/build"><button>{{.T.build}}</button></form>
 <a class="btn" href="{{.PreviewURL}}" target="_blank">{{.T.openPreview}}</a>
 <form method="post" action="/publish" onsubmit="return confirm('{{.T.confirmPublish}}')"><button class="pri">{{.T.publish}}</button></form>
 <a class="btn" href="/settings">{{.T.settings}}</a>
</div>
<h2>{{.T.news}} ({{len .Posts}})</h2>
<table>{{range .Posts}}<tr><td>{{unesc .Title.Rendered}}</td><td>{{date $.Lang .Date}}</td>
<td><a class="btn sm" href="/edit?kind=post&id={{.ID}}">{{$.T.edit}}</a>
<form method="post" action="/delete" onsubmit="return confirm('{{$.T.confirmDelete}}')"><input type="hidden" name="id" value="{{.ID}}"><button class="sm">{{$.T.del}}</button></form></td></tr>{{end}}</table>
<h2>{{.T.pages}}</h2>
<table>{{range .Pages}}<tr><td>{{unesc .Title.Rendered}}</td><td>{{.Path}}</td>
<td><a class="btn sm" href="/edit?kind=page&id={{.ID}}">{{$.T.edit}}</a></td></tr>{{end}}</table>
<p><a class="btn sm" href="/edit?kind=page">{{.T.newPage}}</a></p>
</main></body></html>{{end}}

{{define "edit"}}{{template "head" .}}
<h1>{{if .New}}{{if eq .Kind "page"}}{{.T.newPageTitle}}{{else}}{{.T.newPostTitle}}{{end}}{{else}}{{if eq .Kind "page"}}{{.T.editPageTitle}}{{else}}{{.T.editPostTitle}}{{end}}{{end}}</h1>
<form id="f" method="post" action="/save">
<input type="hidden" name="id" value="{{.P.ID}}"><input type="hidden" name="kind" value="{{.Kind}}">
<label>{{.T.title}}</label><input type="text" name="title" value="{{unesc .P.Title.Rendered}}" required>
<label>{{.T.date}}</label><input type="date" name="date" value="{{day .P.Date}}">
<label>{{.T.content}}</label>
<div class="tb">
<button type="button" onclick="c('bold')"><b>B</b></button><button type="button" onclick="c('italic')"><i>I</i></button>
<button type="button" onclick="c('formatBlock','&lt;h2&gt;')">{{.T.heading}}</button><button type="button" onclick="c('formatBlock','&lt;p&gt;')">{{.T.paragraph}}</button>
<button type="button" onclick="c('insertUnorderedList')">{{.T.list}}</button><button type="button" onclick="lnk()">{{.T.link}}</button>
<button type="button" onclick="document.getElementById('up').click()">{{.T.imagePdf}}</button>
<input id="up" type="file" accept="image/*,.pdf" hidden></div>
<div id="ed" contenteditable="true">{{if .P.ContentLocal}}{{.P.ContentLocal | printf "%s" | safe}}{{end}}</div>
<input type="hidden" name="content" id="content">
<p><button class="pri">{{.T.save}}</button> <a class="btn" href="/">{{.T.cancel}}</a></p>
</form>
<script>
const ed=document.getElementById('ed'), linkPrompt={{.T.linkPrompt}};
function c(a,v){ed.focus();document.execCommand(a,false,v||null)}
function lnk(){const u=prompt(linkPrompt);if(u)c('createLink',u)}
document.getElementById('f').onsubmit=()=>{document.getElementById('content').value=ed.innerHTML}
document.getElementById('up').onchange=async e=>{
 const fd=new FormData();fd.append('file',e.target.files[0]);
 const j=await (await fetch('/upload',{method:'POST',body:fd})).json();
 if(j.error){alert(j.error);return}
 ed.focus();
 if(j.image)document.execCommand('insertImage',false,j.url);
 else document.execCommand('insertHTML',false,'<a href="'+j.url+'">'+j.name+'</a>');
 e.target.value='';
}
</script></main></body></html>{{end}}

{{define "log"}}{{template "head" .}}
<h1>{{if .Failed}}{{.T.failed}}{{else}}{{.T.done}}{{end}}</h1>
<pre>{{range .Lines}}{{.}}
{{end}}</pre>
<p><a class="btn" href="/">{{.T.back}}</a> <a class="btn" href="{{.PreviewURL}}" target="_blank">{{.T.openPreview}}</a></p>
</main></body></html>{{end}}

{{define "settings"}}{{template "head" .}}
<h1>{{.T.settings}}</h1>
<form method="post" action="/settings">
<h2>{{.T.news}}</h2>
<label>{{.T.newsPath}}</label><input type="text" name="news_path" value="{{.NewsPath}}">
<label>{{.T.perPage}}</label><input type="number" name="per_page" value="{{.PerPage}}">
<h2>{{.T.ftpSection}}</h2>
<label>{{.T.server}}</label><input type="text" name="host" value="{{.FTP.Host}}" placeholder="ftp.example.com">
<label>{{.T.port}}</label><input type="number" name="port" value="{{.FTP.Port}}">
<label>{{.T.user}}</label><input type="text" name="user" value="{{.FTP.User}}">
<label>{{.T.pass}}</label><input type="password" name="pass" autocomplete="new-password">
<label>{{.T.serverDir}}</label><input type="text" name="dir" value="{{.FTP.Dir}}" placeholder="/public_html">
<label><input type="checkbox" name="tls" value="1" {{if .FTP.TLS}}checked{{end}}> {{.T.tls}}</label>
<p><button class="pri">{{.T.save}}</button> <a class="btn" href="/">{{.T.cancel}}</a></p>
</form></main></body></html>{{end}}
`

const wizardTemplates = `
{{define "start"}}{{template "head" .}}
<h1>WP2Static</h1>
{{if .Err}}<div class="err">{{.Err}}</div>{{end}}
{{if .Projects}}
<h2>{{.T.yourProjects}}</h2>
<table>{{range .Projects}}<tr><td><b>{{.Name}}</b><br><span class="hint">{{.Site}}</span></td>
<td><form method="post" action="/open"><input type="hidden" name="folder" value="{{.Folder}}"><button class="sm">{{$.T.openBtn}}</button></form></td></tr>{{end}}</table>
{{end}}
<h2>{{.T.newProject}}</h2>
<form method="post" action="/migrate">
<label>{{.T.projectName}}</label>
<input type="text" id="name" name="name" placeholder="{{.T.projectNamePh}}" required autofocus>
<label>{{.T.wpUrl}}</label>
<input type="text" id="url" name="url" placeholder="{{.T.wpUrlPh}}" required>
<label>{{.T.folderLabel}}</label>
<input type="text" id="folder" name="folder" required>
<p class="hint">{{.T.folderHint}}</p>
<p><button class="pri">{{.T.createProject}}</button></p>
</form>
<script>
const base={{.Base}}, sep=base.includes('\\') ? '\\' : '/';
let touched=false; const n=document.getElementById('name'), f=document.getElementById('folder');
f.value=base; f.oninput=()=>touched=true;
n.oninput=()=>{ if(touched) return;
 const slug=n.value.trim().replace(/[\\/:*?"<>|]+/g,'-').replace(/\s+/g,' ').trim();
 f.value = slug ? base+sep+slug : base; };
</script></main></body></html>{{end}}

{{define "job"}}{{template "head" .}}
<h1 id="t">{{.T.migrating}}</h1>
<p class="hint">{{.T.migratingHint}}</p>
<pre id="log">{{.T.starting}}

</pre>
<p id="done" hidden><a class="btn pri" href="/">{{.T.openPanel}}</a></p>
<p id="fail" hidden><a class="btn" href="/start">{{.T.back}}</a></p>
<script>
const tDone={{.T.doneExcl}}, tFail={{.T.failed}};
async function poll(){
 const j=await (await fetch('/job')).json();
 const l=document.getElementById('log'); l.textContent=j.lines.join('\n');
 if(j.done){document.getElementById('t').textContent=j.error?tFail:tDone;
  document.getElementById(j.error?'fail':'done').hidden=false;return}
 setTimeout(poll,1000)}
poll()
</script></main></body></html>{{end}}
`
