package web

func RenderSoftware(version, commit, abi, csrf, target, targetCommit, targetABI string) string {
	review := target != ""
	title := "Software update"
	if review {
		title = "Review update"
	}
	body := pageIntro("Appliance software", title, "")
	body += `<ol class="update-steps" aria-label="Update stages"><li class="current"><span class="step-number">1</span>Choose file</li><li><span class="step-number">2</span>Review</li><li><span class="step-number">3</span>Install</li><li><span class="step-number">4</span>Confirm</li></ol><div class="form-layout"><section class="panel form-panel">`
	if review {
		body += `<span class="hero-badge">` + icon("shield") + ` Signature verified</span><h2 style="margin-top:15px">Install this update?</h2><p class="muted">The package passed signature and compatibility checks. Confirm below to start installation.</p><div class="version-compare"><div class="version-box"><span class="eyebrow">Installed</span><strong>` + esc(version) + `</strong></div>` + icon("arrow") + `<div class="version-box target"><span class="eyebrow">Update</span><strong>` + esc(target) + `</strong></div></div><p class="technical">Architecture: ` + esc(targetABI) + `<br>Source: <code>` + esc(targetCommit) + `</code></p><div class="warning-card">Keep power connected. Log copying pauses during activation. Reconnect after restart to confirm the result.</div><form method="post" enctype="multipart/form-data" action="/software"><input type="hidden" name="csrf_token" value="` + esc(csrf) + `"><input type="hidden" name="commit" value="` + esc(targetCommit) + `"><div class="form-actions"><button class="primary" name="install" value="1">Install update ` + icon("arrow") + `</button><a class="btn" href="/software">Cancel</a></div></form>`
	} else {
		body += `<h2>Update file</h2><p class="muted">Choose a signed .lfu package. Maximum 32 MiB.</p><form id="upload-form" method="post" action="/software" enctype="multipart/form-data"><input type="hidden" name="csrf_token" value="` + esc(csrf) + `"><label class="upload-zone">` + icon("upload") + `<strong>Choose an update file</strong><small>.lfu package · maximum 32 MiB</small><span class="file-name" id="file-name">No file selected</span><input id="update-file" aria-label="Update file" type="file" name="package" accept=".lfu" required></label><div class="security-note">` + icon("shield") + `<span>Signature and compatibility are checked before installation.</span></div><button class="primary">Upload and review ` + icon("arrow") + `</button></form><section class="update-result" aria-live="polite"><h3 id="update-title">Checking update state…</h3><p id="result">Checking status…</p></section>`
	}
	body += `</section><aside class="aside-stack"><section class="panel side-panel"><span class="eyebrow">On this appliance</span><h2>` + esc(version) + `</h2><p class="field-note">Installed application version</p><details class="technical"><summary>Build details</summary><p>Source: <code>` + esc(commit) + `</code></p><p>Application ABI: <code>` + esc(abi) + `</code></p></details></section><details class="panel side-panel help-card"><summary>Update help</summary><p class="field-note">Keep power connected. After restart, reconnect to Wi-Fi and check the result here.</p><p class="field-note">Logs and settings are preserved. Raspberry Pi OS is not updated.</p><p class="field-note">Installation is confirmed only after the application reports healthy.</p></details></aside></div>`
	script := softwareJS
	if review {
		script = `document.querySelectorAll('.update-steps li').forEach((li,i)=>li.className=i===0?'done':i===1?'current':'');`
	}
	return renderShell("Software update", "software", version, body, script)
}

const softwareJS = `
let uploading=false;
const upload=document.getElementById('upload-form'),file=document.getElementById('update-file'),result=document.getElementById('result'),title=document.getElementById('update-title');
file.addEventListener('change',()=>{const f=file.files[0];file.setCustomValidity(f&&f.size>32*1024*1024?'Choose a file no larger than 32 MiB.':'');document.getElementById('file-name').textContent=f?f.name:'No file selected';});
upload.addEventListener('submit',()=>{uploading=true;title.textContent='Checking your update file';result.textContent='Uploading for signature and compatibility checks. Keep this page open.';upload.querySelector('button').disabled=true;});
const phases={idle:['Ready for an update',1],queued:['Update queued',3],validating:['Checking update',3],activating:['Installing update',3],restarting:['Restarting appliance',4],success:['Update confirmed',4],failed:['Update failed',0],'rolled-back':['Previous version restored',0]};
async function poll(){if(uploading)return;try{const r=await fetch('/software/status',{cache:'no-store'});if(!r.ok)throw Error();const s=await r.json(),p=phases[s.Phase]||['Update result unconfirmed',0];title.textContent=p[0];result.textContent=(s.Message||'No confirmed update result is available.')+(s.Version?' · '+s.Version:'');document.querySelectorAll('.update-steps li').forEach((li,i)=>li.className=i+1===p[1]?'current':i+1<p[1]?'done':'');const busy=['queued','validating','activating','restarting'].includes(s.Phase);file.disabled=busy;upload.querySelector('button').disabled=busy;}catch{title.textContent='Connection unavailable';result.textContent='Installation is unconfirmed. Reconnect to the appliance’s Wi-Fi and reload this page.';}}
poll();setInterval(poll,2000);
`
