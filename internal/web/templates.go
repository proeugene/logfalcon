// Package web provides the LogFalcon HTTP server and embedded HTML templates.
package web

import (
	"encoding/json"
	"fmt"
	"html"
	"sort"
	"strings"
	"time"

	"github.com/proeugene/logfalcon/internal/storage"
	lfSync "github.com/proeugene/logfalcon/internal/sync"
)

type IndexParams struct {
	Version, DeviceName                         string
	UsedGB, FreeGB                              float64
	Pct                                         int
	SessionsHTML, StorageWarningHTML, CSRFToken string
	Status                                      lfSync.Status
}
type SettingsParams struct {
	Country, CurrentSSID, CurrentPass, MsgHTML, WarningHTML, CSRFToken, Version string
	Setup                                                                       bool
}

func esc(s string) string { return html.EscapeString(s) }

type statusPresentation struct {
	Title, Body, Label, Tone string
	Step                     int
	Active                   bool
}

var statusPresentations = map[string]statusPresentation{
	"waiting":     {"Waiting for status", "No transfer reported. Connect a controller to start.", "Awaiting status", "waiting", 0, false},
	"idle":        {"Waiting for controller", "Connect a controller to copy its logs.", "Waiting for controller", "waiting", 0, false},
	"identifying": {"Connecting", "Identifying your controller. Keep the USB cable connected.", "Connecting", "active", 1, true},
	"querying":    {"Checking controller", "Checking controller storage and available space. Keep the controller connected.", "Connecting", "active", 1, true},
	"syncing":     {"Copying logs", "Copying your controller’s logs to the appliance. Keep it connected until completion.", "Copying logs", "active", 2, true},
	"verifying":   {"Checking saved copy", "Comparing the received bytes with the saved file. Keep the controller connected.", "Checking saved copy", "active", 3, true},
	"erasing":     {"Erasing controller", "Your copy is saved. Keep the controller connected while its requested erase finishes.", "Erase in progress", "warning", 3, true},
	"complete":    {"Copy completed.", "The recording is ready to download.", "Copy completed", "complete", 4, false},
	"empty":       {"No logs to copy", "The controller reports empty flash. Record a flight, then connect it again.", "Controller flash empty", "waiting", 0, false},
	"interrupted": {"Copy interrupted", "The transfer was interrupted. Reconnect your controller to start a new copy. Partial evidence is retained.", "Copy interrupted", "warning", 0, false},
	"error":       {"Copy failed", "Completion is not confirmed. Preserve the controller’s data and check the transfer details below.", "Copy failed", "error", 0, false},
	"unavailable": {"Status is unavailable.", "Completion is not confirmed. Check the appliance connection and reload this page.", "Connection needs attention", "warning", 0, false},
}

func presentation(state string) statusPresentation {
	if p, ok := statusPresentations[state]; ok {
		return p
	}
	return statusPresentations["unavailable"]
}
func boundedPercent(n int) int {
	if n < 0 {
		return 0
	}
	if n > 100 {
		return 100
	}
	return n
}

func RenderIndex(p IndexParams) string {
	view := presentation(p.Status.State)
	hidden := ""
	if !view.Active && p.Status.State != "complete" {
		hidden = " hidden"
	}
	var steps strings.Builder
	for i, label := range []string{"Connect", "Copy logs", "Check copy", "Completed"} {
		cls := ""
		if i+1 < view.Step || view.Step == 4 {
			cls = "done"
		} else if i+1 == view.Step {
			cls = "current"
		}
		fmt.Fprintf(&steps, `<li class="%s"><span class="step-number">%d</span><span>%s</span></li>`, cls, i+1, label)
	}
	device := p.DeviceName
	if device == "" {
		device = "LogFalcon"
	}
	body := pageIntro("", "Recordings", "")
	body += fmt.Sprintf(`<section id="transfer-hero" class="panel hero" data-tone="%s" aria-labelledby="transfer-title"><div class="hero-main"><div class="hero-copy"><span class="hero-badge"><span class="dot"></span><span id="hero-label">%s</span></span><h2 id="transfer-title" aria-live="polite">%s</h2><p class="hero-description" id="hero-description">%s</p><a id="hero-action" class="btn primary" href="#connection-help">%sConnection help</a><span id="keep-connected" class="badge" hidden>Keep controller connected</span></div><div class="hero-art" aria-hidden="true"><span class="orbit"></span>%s</div></div><div id="copy-progress" class="copy-progress"%s><div class="progress-numbers"><strong class="progress-number" id="progress-number">%d%%</strong><span class="progress-meta" id="progress-meta">Keep the controller connected</span></div><div class="track"><div id="progress-fill" class="fill" role="progressbar" aria-label="Copy progress" aria-valuemin="0" aria-valuemax="100" aria-valuenow="%d" style="width:%d%%"></div></div></div><ol class="steps" id="copy-steps" aria-label="Transfer stages">%s</ol></section>`,
		esc(view.Tone), esc(view.Label), esc(view.Title), esc(view.Body), icon("help"), falconMark, hidden, boundedPercent(p.Status.Progress), boundedPercent(p.Status.Progress), boundedPercent(p.Status.Progress), steps.String())
	body += `<div class="warning-card" id="transfer-warning" role="status"` + func() string {
		if p.Status.Warning == "" {
			return " hidden"
		}
		return ""
	}() + `>` + esc(p.Status.Warning) + `</div>`
	body += p.StorageWarningHTML
	body += `<div class="dashboard-grid"><section id="saved-logs" aria-labelledby="logs-title"><div class="section-heading"><h2 id="logs-title">Saved recordings</h2><button class="text-link secondary" id="refresh-sessions" type="button" aria-label="Refresh saved recordings">` + icon("file") + `Refresh list</button></div><details class="recording-guidance"><summary>About these recordings</summary><p class="field-note">Each copy may contain several flights. Times show when it was collected. Copies can overlap if controller data is retained.</p><ol class="field-note"><li>Download its .bbl file to your phone or computer.</li><li>Open Betaflight App’s Blackbox Viewer and load the file.</li><li>Use your notes to find the test you want to review.</li></ol><p class="field-note">Saved-copy checks do not confirm that a recording is complete, decodable or useful for diagnosis. <a href="https://app.betaflight.com/" target="_blank" rel="noopener noreferrer">Open Betaflight App ↗</a> (internet access or prior installation may be needed). Downloads are not opened or uploaded there automatically.</p></details><p id="list-result" class="field-hint" role="status" hidden></p><form id="export-form" method="post" action="/download-selected" target="export-frame"><input type="hidden" name="csrf_token" value="` + esc(p.CSRFToken) + `"><div class="selection-bar"><label class="select-all"><input type="checkbox" id="select-all"> Select all</label><button id="export-button" class="primary" disabled>Download selected (0)</button></div><p id="export-result" class="field-hint" role="status" hidden></p></form><iframe id="export-frame" name="export-frame" title="Download response" hidden></iframe><div id="saved-sessions">` + p.SessionsHTML + `</div></section><aside class="aside-stack" aria-label="Appliance information">`
	body += fmt.Sprintf(`<section class="panel side-panel disk-info"><span class="eyebrow">%s Appliance storage</span><div class="storage-number">%.1f <small>GB free</small></div><div class="disk-bar-track"><div class="disk-bar-fill" style="width:%d%%"></div></div><p class="storage-info">%.1f GB used</p></section>`, icon("storage"), p.FreeGB, boundedPercent(p.Pct), p.UsedGB)
	body += `<section class="panel side-panel"><span class="eyebrow">` + icon("wifi") + ` Your appliance</span><h3>` + esc(device) + `</h3><a class="text-link" href="/settings">Wi-Fi settings ` + icon("arrow") + `</a></section><details class="panel side-panel help-card" id="connection-help"><summary>Connection &amp; download help</summary><ol><li>Join your appliance’s Wi-Fi and open <a href="http://192.168.4.1">http://192.168.4.1</a>. Stay connected if your phone reports no internet.</li><li>Connect the controller to the USB / OTG data port. The other port powers the Pi.</li><li>Wait for completion, download the .bbl file and open it in Blackbox Explorer.</li><li>During evaluation, keep controller erase and storage cleanup disabled.</li></ol></details><section class="side-panel"><div class="side-links"><a href="/software">Software update ↗</a><details><summary>Transfer details</summary><p id="transfer-status" class="technical" role="status">` + esc(p.Status.Message) + `</p></details></div></section></aside></div>`
	jsonViews, _ := json.Marshal(statusPresentations)
	initial, _ := json.Marshal(p.Status)
	script := "const presentations=" + string(jsonViews) + ";const initialStatus=" + string(initial) + ";const csrfToken=" + strconvQuote(p.CSRFToken) + ";" + dashboardJS
	return renderShell("Dashboard", "dashboard", p.Version, body, script)
}
func strconvQuote(s string) string { b, _ := json.Marshal(s); return string(b) }

const dashboardJS = `
const byId=id=>document.getElementById(id);
const setText=(id,value)=>{const el=byId(id);if(el&&el.textContent!==value)el.textContent=value};
let completedTransfer=null,polling=false;
function formatBytes(n){return n>=1048576?(n/1048576).toFixed(1)+' MB':Math.round(n/1024)+' KB'}
function applyStatus(s){
 const v=presentations[s.state]||presentations.unavailable;
 byId('transfer-hero').dataset.tone=v.Tone;
 setText('hero-label',v.Label);setText('transfer-title',v.Title);setText('hero-description',v.Body);
 setText('transfer-status',s.message||'No additional transfer details.');
 byId('transfer-warning').hidden=!s.warning;setText('transfer-warning',s.warning||'');
 const pct=Math.max(0,Math.min(100,Number(s.progress)||0));
 byId('copy-progress').hidden=!v.Active&&s.state!=='complete';
 setText('progress-number',pct+'%');
 byId('progress-fill').style.width=pct+'%';byId('progress-fill').setAttribute('aria-valuenow',pct);
 const stats=[];
 if(s.total_bytes>0)stats.push(formatBytes(s.bytes_copied||0)+' / '+formatBytes(s.total_bytes));
 if(s.state==='syncing'&&s.speed_bps>0)stats.push(formatBytes(s.speed_bps)+'/s');
 if(s.state==='syncing'&&s.eta_sec>0)stats.push('~'+Math.ceil(s.eta_sec)+'s left');
 setText('progress-meta',stats.join(' · ')||(s.state==='complete'?'Saved copy confirmed':'Keep the controller connected'));
 byId('copy-steps').querySelectorAll('li').forEach((li,i)=>{
  li.classList.toggle('done',i+1<v.Step||v.Step===4);li.classList.toggle('current',i+1===v.Step&&v.Step!==4);
  if(i+1===v.Step)li.setAttribute('aria-current','step');else li.removeAttribute('aria-current');
 });
 byId('keep-connected').hidden=!v.Active;
 const action=byId('hero-action');action.hidden=v.Active;
 if(s.state==='complete'){action.href='#saved-logs';action.textContent='View recordings'}
 else if(s.state==='error'||s.state==='interrupted'||s.state==='unavailable'){action.href='#connection-help';action.textContent='Connection help'}
 else{action.href='#connection-help';action.textContent='Connection help'}
}
async function refreshSessions(manual=false){
 const response=await fetch('/sessions/fragment',{cache:'no-store'});
 if(!response.ok)throw new Error('Saved logs unavailable');
 const fresh=document.createElement('div');fresh.innerHTML=await response.text();
 const selected=new Set(Array.from(document.querySelectorAll('.recording-select:checked'),el=>el.value));
 let container=byId('session-list');
 if(!container){container=document.createElement('div');container.id='session-list';container.className='session-list';byId('saved-sessions').append(container)}
 const oldCards=Array.from(container.querySelectorAll('.session-card'));
 const incoming=Array.from(fresh.querySelectorAll('.session-card'));
 for(const old of oldCards){
  if(!incoming.some(card=>card.dataset.session===old.dataset.session)&&!old.contains(document.activeElement)&&!old.querySelector('[data-delete-busy]'))old.remove();
 }
 for(const card of incoming.reverse()){
  const old=oldCards.find(c=>c.dataset.session===card.dataset.session);
  if(!old)container.prepend(card);
  else if(old.dataset.revision!==card.dataset.revision&&!old.contains(document.activeElement)){
   card.querySelector('.session-details').open=old.querySelector('.session-details').open;old.replaceWith(card);
  }
 }
 container.querySelectorAll('.recording-select').forEach(el=>el.checked=selected.has(el.value));updateSelection();
 if(container.querySelector('.session-card'))byId('saved-sessions').querySelector('.empty-state')?.remove();
 else if(!byId('saved-sessions').querySelector('.empty-state')){const empty=fresh.querySelector('.empty-state');if(empty)byId('saved-sessions').prepend(empty);}
 if(manual){byId('list-result').hidden=false;setText('list-result','Saved logs are up to date.')}
}
async function updateStatus(){
 if(polling)return;polling=true;
 try{
  const response=await fetch('/status',{cache:'no-store'});if(!response.ok)throw new Error('Status unavailable');
  const s=await response.json();applyStatus(s);
  if(s.state==='complete'&&completedTransfer!==s.transfer_id&&!document.querySelector('[data-delete-busy]')){
   try{await refreshSessions();completedTransfer=s.transfer_id}
   catch{byId('list-result').hidden=false;setText('list-result','Copy completed. Could not refresh saved logs; retry Refresh list.')}
  }
 }catch{applyStatus({state:'unavailable',message:'Connection lost. Completion is not confirmed; reconnect to the appliance.'})}
 finally{polling=false}
}
byId('hero-action').addEventListener('click',e=>{if(e.currentTarget.hash==='#connection-help')byId('connection-help').open=true});
byId('refresh-sessions').addEventListener('click',async()=>{
 try{await refreshSessions(true)}catch{byId('list-result').hidden=false;setText('list-result','Could not refresh. Check your appliance connection and try again.')}
});
document.addEventListener('click',async e=>{
 const btn=e.target.closest('[data-delete-session]');if(!btn)return;
 if(!confirm('Delete this saved session from the Pi?\n\nDownload the .bbl first. The controller is not changed.'))return;
 btn.disabled=true;btn.dataset.deleteBusy='true';btn.textContent='Deleting…';
 try{
  const r=await fetch('/sessions/'+btn.dataset.deleteSession,{method:'DELETE',headers:{'X-CSRF-Token':csrfToken}});
  if(!r.ok)throw new Error(r.status===403?'This page expired. Reload before trying again.':'Deletion is unconfirmed. Check your connection.');
  const result=await r.json();if(!result.deleted)throw new Error('Deletion is unconfirmed.');
  location.reload();
 }catch(err){btn.disabled=false;delete btn.dataset.deleteBusy;btn.textContent='Delete from Pi';alert(err.message)}
});
function updateSelection(){
 const boxes=Array.from(document.querySelectorAll('.recording-select:not(:disabled)')),n=boxes.filter(el=>el.checked).length;
 byId('select-all').checked=n>0&&n===boxes.length;byId('select-all').indeterminate=n>0&&n<boxes.length;
 byId('select-all').disabled=!boxes.length;byId('export-button').disabled=n===0||n>100;
 byId('export-button').textContent='Download selected ('+n+')';
 if(n>100){byId('export-result').hidden=false;setText('export-result','Select up to 100 recordings per ZIP.');}
}
document.addEventListener('change',e=>{if(e.target.matches('.recording-select'))updateSelection()});
byId('select-all').addEventListener('change',e=>{
 const boxes=Array.from(document.querySelectorAll('.recording-select:not(:disabled)')),checked=e.target.checked;
 boxes.forEach((el,i)=>el.checked=checked&&i<100);updateSelection();
 if(boxes.length>100&&checked){byId('export-result').hidden=false;setText('export-result','Selected the first 100 recordings.');}
});
byId('export-form').addEventListener('submit',()=>{byId('export-result').hidden=false;setText('export-result','ZIP requested. Check your browser’s downloads.');});
byId('export-frame').addEventListener('load',()=>{
 try{const text=byId('export-frame').contentDocument.body.textContent;if(!text)return;const result=JSON.parse(text);if(result.error){byId('export-result').hidden=false;setText('export-result',result.error);}}catch{}
});
updateSelection();applyStatus(initialStatus);setInterval(updateStatus,2000);

`

func controllerLabel(dir string) string {
	variant, uid, ok := strings.Cut(strings.TrimPrefix(dir, "fc_"), "_uid-")
	if !ok {
		return dir
	}
	switch variant {
	case "BTFL":
		variant = "Betaflight"
	case "INAV":
		variant = "iNav — Experimental; copying currently disabled"
	}
	return variant + " · controller " + uid
}
func sessionDate(s *storage.Session) string {
	stamp := s.SessionDir
	// Names have a 17-character UTC timestamp followed by a collision suffix.
	if len(s.SessionDir) >= 17 {
		stamp = s.SessionDir[:17]
	}
	if t, err := time.Parse("2006-01-02_150405", stamp); err == nil {
		return t.Format("2 Jan 2006 · 15:04") + " UTC"
	}
	return strings.ReplaceAll(s.SessionDir, "_", " ")
}
func outcome(s *storage.Session) (string, string, string) {
	m := s.Manifest
	if m != nil && m.EraseCompleted {
		return "erased", "FC erased", "The manifest records a completed controller erase."
	}
	if m != nil && m.EraseAttempted {
		return "unconfirmed", "Erase unconfirmed", "An erase was attempted; completion was not confirmed."
	}
	if m != nil && m.ControllerDataRetained {
		return "retained", "Controller data retained", "The manifest records copying without an erase attempt."
	}
	return "no-erase", "No erase recorded", "The manifest does not record a confirmed retention or erase outcome."
}
func RenderSessions(sessions []*storage.Session) string {
	if len(sessions) == 0 {
		return `<div class="empty-state"><div class="empty-icon">` + icon("file") + `</div><strong>No saved recordings yet.</strong><p>Connect a controller to copy its logs.</p><a class="text-link" href="#connection-help" onclick="document.getElementById('connection-help').open=true">Connection help ` + icon("arrow") + `</a></div>`
	}
	ordered := append([]*storage.Session(nil), sessions...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].SessionDir > ordered[j].SessionDir })
	var b strings.Builder
	b.WriteString(`<div class="session-list" id="session-list">`)
	for _, s := range ordered {
		cls, label, tip := outcome(s)
		size := int64(0)
		api := "?"
		sha := ""
		if s.Manifest != nil {
			size = s.Manifest.File.Bytes
			api = s.Manifest.FC.APIVersion
			sha = s.Manifest.File.SHA256
		}
		download := `<span class="field-hint">Log file unavailable.</span>`
		if s.BBLPath != nil {
			download = fmt.Sprintf(`<a class="btn session-download" href="/download/%s/raw_flash.bbl" aria-label="Download .bbl from %s">%s</a>`, esc(s.SessionID), esc(sessionDate(s)), icon("download"))
		}
		fmt.Fprintf(&b, `<article class="session-card" data-session="%s" data-revision="%s"><div class="session-main">%s<div><span class="session-title">%s</span><div class="session-controller">%s</div></div>%s</div><div class="session-foot"><span class="badge %s" title="%s">%s</span><span class="session-size">%.1f MB · .BBL</span></div>%s<details class="session-details"><summary>Transfer evidence and actions</summary><div class="session-meta"><span>MSP API %s</span><span>SHA-256: %s</span><span>SHA-256 is saved-copy evidence; no independent controller checksum is recorded.</span></div><div class="session-actions"><a class="btn" href="/download/%s/manifest.json">Download manifest</a><button class="btn-delete secondary" data-delete-session="%s" type="button">Delete from Pi</button></div></details></article>`,
			esc(s.SessionID), recordingRevision(s), recordingCheckbox(s), esc(recordingTitle(s)), esc(recordingController(s)), download, cls, esc(tip), esc(label), float64(size)/1048576, recordingContext(s), esc(api), esc(sha), esc(s.SessionID), esc(s.SessionID))
	}
	b.WriteString("</div>")
	return b.String()
}

func RenderSettings(p SettingsParams) string {
	title := "Set up Wi-Fi"
	intro := ""
	if !p.Setup {
		title = "Settings"
		intro = ""
	}
	body := pageIntro("WI-FI & APPLIANCE", title, intro)
	body += p.MsgHTML + p.WarningHTML
	body += `<div class="form-layout"><section class="panel form-panel" aria-labelledby="wifi-title"><h2 id="wifi-title">Wi-Fi settings</h2>`
	if p.Setup {
		body += `<p class="onboarding-note">Optional. You can skip setup and start copying.</p>`
	}
	current := p.CurrentSSID
	if current == "" || current == "Unknown" {
		current = "Configuration unavailable"
	}
	body += `<div class="current-network">` + icon("wifi") + `<div><small>Current Wi-Fi name</small><strong>` + esc(current) + `</strong></div></div><form id="wifi-form" method="POST" action="/settings"><input type="hidden" name="csrf_token" value="` + esc(p.CSRFToken) + `"><div class="form-group"><label for="ssid">Wi-Fi name</label><input type="text" id="ssid" name="ssid" value="` + esc(p.CurrentSSID) + `" required minlength="1" maxlength="32" pattern="[ -~]{1,32}" autocomplete="off"><small class="field-hint">1–32 printable characters.</small></div><div class="form-group"><label for="password">New password <span class="muted">· optional</span></label><div class="password-wrap"><input type="password" id="password" name="password" minlength="8" maxlength="63" pattern="[ -~]{8,63}" autocomplete="new-password" placeholder="Leave blank to keep your current password"><button type="button" class="password-toggle" id="toggle-password" aria-label="Show new password" aria-pressed="false">` + icon("eye") + `</button></div><small class="field-hint">8–63 printable characters. Spaces are preserved.</small></div><div class="form-group"><label for="country">Country</label><input type="text" id="country" name="country" value="` + esc(p.Country) + `" list="country-options" required pattern="[A-Z]{2}" maxlength="2" autocomplete="off" placeholder="GB"><datalist id="country-options">`
	for _, c := range []struct{ code, name string }{{"GB", "United Kingdom"}, {"US", "United States"}, {"CA", "Canada"}, {"AU", "Australia"}, {"NZ", "New Zealand"}, {"IE", "Ireland"}, {"FR", "France"}, {"DE", "Germany"}, {"ES", "Spain"}, {"IT", "Italy"}, {"NL", "Netherlands"}, {"BE", "Belgium"}, {"PT", "Portugal"}, {"PL", "Poland"}, {"UA", "Ukraine"}, {"CH", "Switzerland"}, {"AT", "Austria"}, {"SE", "Sweden"}, {"NO", "Norway"}, {"DK", "Denmark"}, {"FI", "Finland"}, {"JP", "Japan"}, {"ZA", "South Africa"}} {
		body += `<option value="` + c.code + `" label="` + c.name + `"></option>`
	}
	body += `</datalist><small class="field-hint">Two-letter country code.</small></div><div class="security-note">` + icon("shield") + `<span>Anyone connected to this Wi-Fi can administer the appliance. Use a private password before shared use.</span></div><div class="form-actions"><button type="submit" class="primary" id="save-wifi">Review Wi-Fi changes ` + icon("arrow") + `</button>`
	if p.Setup {
		body += `<button type="submit" name="skip" value="1" class="secondary" formnovalidate>Skip for now</button>`
	}
	body += `</div></form></section><aside class="aside-stack"><section class="panel side-panel"><span class="eyebrow">` + icon("wifi") + ` When you save</span><h3>Wi-Fi will restart</h3><p class="field-note">Reconnect using your new details, then open <strong>http://192.168.4.1/settings</strong> to check them.</p><p class="field-note">A lost connection does not confirm that settings were applied.</p></section><details class="panel side-panel help-card"><summary>Need to recover Wi-Fi?</summary><p class="field-note">You can edit logfalcon-config.txt on the SD card’s boot partition, then reboot. Preserve your existing settings and change only the values you intend to update.</p></details><section class="panel side-panel"><span class="eyebrow">SOFTWARE</span><h3>Software update</h3><p class="field-note">Install a signed .lfu package.</p><a class="text-link" href="/software">Software update ` + icon("arrow") + `</a></section></aside></div><dialog id="wifi-review" aria-labelledby="wifi-review-title"><span class="eyebrow">REVIEW & RECONNECT</span><h2 id="wifi-review-title">Save Wi-Fi settings?</h2><p>Wi-Fi will restart. Reconnect using these details.</p><dl class="review-list"><dt>Wi-Fi name</dt><dd id="review-ssid"></dd><dt>Password</dt><dd><span id="review-password">Current password unchanged</span> <button type="button" class="text-link secondary" id="reveal-review" hidden>Show</button></dd><dt>Country</dt><dd id="review-country"></dd></dl><p>Reconnect and open <strong>http://192.168.4.1/settings</strong> to confirm the effective details. If the connection is lost, application remains unconfirmed until you reconnect.</p><div class="dialog-actions"><button class="primary" type="button" id="apply-wifi">Save and restart Wi-Fi</button><button class="secondary" type="button" id="cancel-wifi">Go back</button></div></dialog>`
	return renderShell("Settings", "settings", p.Version, body, settingsJS)
}

const settingsJS = `
const wifiForm=document.getElementById('wifi-form'),review=document.getElementById('wifi-review'),password=document.getElementById('password');
document.getElementById('toggle-password').addEventListener('click',e=>{
 const reveal=password.type==='password';password.type=reveal?'text':'password';
 e.currentTarget.setAttribute('aria-label',reveal?'Hide new password':'Show new password');e.currentTarget.setAttribute('aria-pressed',reveal);
});
document.getElementById('country').addEventListener('input',e=>{e.target.value=e.target.value.toUpperCase()});
wifiForm.addEventListener('submit',e=>{
 if(e.submitter?.name==='skip')return;
 e.preventDefault();document.getElementById('review-ssid').textContent=document.getElementById('ssid').value;
 document.getElementById('review-country').textContent=document.getElementById('country').value;
 document.getElementById('review-password').textContent=password.value?'New password entered (hidden)':'Current password unchanged';
 document.getElementById('reveal-review').hidden=!password.value;document.getElementById('reveal-review').textContent='Show';
 review.showModal();
});
document.getElementById('reveal-review').addEventListener('click',e=>{
 const reveal=e.currentTarget.textContent==='Show';document.getElementById('review-password').textContent=reveal?password.value:'New password entered (hidden)';e.currentTarget.textContent=reveal?'Hide':'Show';
});
document.getElementById('cancel-wifi').addEventListener('click',()=>review.close());
document.getElementById('apply-wifi').addEventListener('click',()=>{document.getElementById('apply-wifi').disabled=true;document.getElementById('apply-wifi').textContent='Applying… reconnect after Wi-Fi restarts';wifiForm.submit()});
`

func RenderError(code int, reason string) string {
	body := pageIntro(fmt.Sprintf("REQUEST %d", code), "Request failed", reason) + `<section class="panel form-panel"><p class="muted">If the appliance restarted, reconnect to its Wi-Fi and reload the page before trying again.</p><a class="btn primary" href="/">Back to dashboard ` + icon("arrow") + `</a><a class="btn secondary" href="/software">Software update</a></section>`
	return renderShell("Request needs attention", "", "", body, "")
}
