package web

import (
	"fmt"
	"io/fs"
	"strings"

	"github.com/proeugene/logfalcon/internal/storage"
	lfSync "github.com/proeugene/logfalcon/internal/sync"
)

type capturePanel struct {
	Summary       string
	Channels      string
	DetailsHref   string
	ReferenceHref string
}

func capturePanelHTML(p capturePanel) string {
	hidden := ""
	if p.Summary == "" {
		hidden = " hidden"
	}
	link := func(id, label, href string) string {
		attrs := " hidden"
		if href != "" {
			attrs = ` href="` + esc(href) + `"`
		}
		return `<a class="text-link" id="` + id + `"` + attrs + `>` + label + `</a>`
	}
	return `<div class="recording-context" id="capture-panel"` + hidden +
		`><p id="capture-summary" aria-live="polite">` + esc(strings.TrimSpace(p.Summary+" "+p.Channels)) + `</p>` +
		link("capture-details", "Channel details", p.DetailsHref) + " " +
		link("capture-reference", "Reference copy", p.ReferenceHref) + `</div>`
}

func validCaptureSession(id string) bool {
	return fs.ValidPath(id) && strings.Count(id, "/") == 1 && !strings.ContainsAny(id, `\:`)
}

func (s *Server) completedCapture(status lfSync.Status) capturePanel {
	saved := status.SavedCopy
	if status.State != "complete" || saved == nil || saved.Bytes <= 0 || !validCaptureSession(saved.SessionID) {
		return capturePanel{}
	}
	check, err := storage.ReadCaptureCheck(s.storagePath, saved.SessionID)
	if err != nil || check.Bytes != saved.Bytes {
		return capturePanel{Summary: "Channel check unavailable."}
	}
	return presentCapture(check)
}

func gyroGroup(name string, axes uint8) string {
	if axes == 0 {
		return "No " + strings.ToLower(name) + " gyro fields."
	}
	var names []string
	for i, axis := range []string{"X", "Y", "Z"} {
		if axes&(1<<i) != 0 {
			names = append(names, axis)
		}
	}
	return name + " gyro on " + strings.Join(names, ", ") + "."
}

func gyroChannels(h storage.RecordingHeader) string {
	if h.Gyro == nil {
		return "Gyro fields unknown."
	}
	return gyroGroup("Filtered", h.Gyro.Filtered) + " " + gyroGroup("Unfiltered", h.Gyro.Unfiltered)
}

func presentCapture(check *storage.CaptureCheck) capturePanel {
	p := capturePanel{Summary: "No comparison available.", DetailsHref: recordingURL(check.SessionID)}
	c := check.Comparison
	records := check.Recordings
	// ReadCaptureCheck validates the comparison and both session identities.
	if c.ReferenceSessionID != "" {
		p.ReferenceHref = recordingURL(c.ReferenceSessionID)
	}
	switch c.Outcome {
	case storage.CaptureIdentical:
		p.Summary = "Identical to the reference copy."
	case storage.CaptureChanged:
		p.Summary = "Flash contents changed. New recordings could not be confirmed."
	case storage.CaptureAppended:
		count, last := 0, 0
		uncertain := c.IndexLimited
		for i, h := range records {
			if c.IsNewHeader(h) {
				count++
				last = i
			} else if h.Offset >= c.ReferenceBytes {
				uncertain = true
			}
		}
		switch {
		case uncertain:
			p.Summary = "New recording headers found. Total unconfirmed."
		case count == 1:
			p.Summary = fmt.Sprintf("Recording header %d is new.", last+1)
		default:
			p.Summary = fmt.Sprintf("%d new recording headers.", count)
		}
		if count == 1 {
			p.Channels = gyroChannels(records[last])
		}
	case storage.CaptureUnavailable:
		if len(records) > 0 {
			p.Channels = fmt.Sprintf("Header %d. %s", len(records), gyroChannels(records[len(records)-1]))
		}
	}
	if c.IndexLimited {
		p.Summary += " Header index limit reached."
	}
	return p
}

func captureDetails(check *storage.CaptureCheck) string {
	var b strings.Builder
	b.WriteString(`<section class="panel form-panel"><h2>Blackbox capture check</h2>`)
	if check == nil {
		b.WriteString(`<p>Channel check unavailable.</p></section>`)
		return b.String()
	}
	p := presentCapture(check)
	b.WriteString("<p>" + esc(p.Summary) + "</p>")
	if p.ReferenceHref != "" {
		fmt.Fprintf(&b, `<p class="technical">Reference copy <a href="%s">%s</a></p>`, esc(p.ReferenceHref), esc(check.Comparison.ReferenceSessionID))
	}
	if len(check.Recordings) == 0 {
		b.WriteString("<p>No recording headers identified.</p>")
	} else {
		b.WriteString(`<p class="field-note">Fields declared in each recording header. Sample data has not been decoded.</p><ol>`)
		for _, h := range check.Recordings {
			b.WriteString("<li>")
			if check.Comparison.IsNewHeader(h) {
				b.WriteString("<strong>New.</strong> ")
			}
			b.WriteString(esc(gyroChannels(h)) + "</li>")
		}
		b.WriteString("</ol>")
	}
	b.WriteString("</section>")
	return b.String()
}
