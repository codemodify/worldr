package projectapp

import (
	"image"
	"strings"
	"time"

	"github.com/codemodify/worldr/internal/experience"
)

const autoRefreshInterval = 2 * time.Second
const maxSearchBytes = 256

func searchRect(width int) image.Rectangle      { return image.Rect(14, 34, width-14, 56) }
func searchFieldRect(width int) image.Rectangle { return image.Rect(126, 34, width-14, 56) }

func (p *Provider) selectionName() string {
	if p.selected >= 0 && p.selected < len(p.entries) {
		return p.entries[p.selected].name
	}
	return ""
}

func (p *Provider) installListing(entries []entry, selected string, preserve bool) {
	old, hadOld := entry{}, p.selected >= 0 && p.selected < len(p.entries)
	if hadOld {
		old = p.entries[p.selected]
	}
	p.allEntries = entries
	filtered := make([]entry, 0, len(entries))
	query := strings.ToLower(p.query)
	for _, item := range entries {
		matchesQuery := query == "" || strings.Contains(strings.ToLower(item.name), query)
		matchesIntent := p.openIntent == OpenAny || item.kind == directoryEntry || item.kind == fileEntry && p.openIntent.accepts(item.name)
		if matchesQuery && matchesIntent {
			filtered = append(filtered, item)
		}
	}
	p.entries = filtered
	index := -1
	if len(filtered) > 0 {
		index = 0
		for i, item := range filtered {
			if item.name == selected {
				index = i
				break
			}
		}
	}
	if preserve && hadOld && index >= 0 && old == filtered[index] && p.previewPath != "" {
		p.selected = index
		p.clampScroll()
		p.ensureSelectionVisible()
		return
	}
	p.selected = -1
	if index < 0 {
		p.preview, p.previewPath = textPreview{}, ""
		p.previewTop, p.previewLeft, p.listTop = 0, 0, 0
		p.message = p.openIntent.emptyMessage()
		if p.query != "" {
			p.message = "No filenames match this search in the listed folder."
		}
		return
	}
	notice := p.notice
	p.selectEntry(index)
	p.notice = notice
}

func (p *Provider) setQuery(query string) {
	if len(query) > maxSearchBytes || query == p.query {
		return
	}
	p.query, p.dirty = query, true
	if p.searchField.Text() != query {
		p.searchField.Set(query)
	}
	if p.loadingDirectory && p.allEntries == nil {
		return // Keep the initial listing and its saved selection pending.
	}
	selected := p.selectionName()
	if p.loadingFile {
		p.previewPath = ""
	}
	if p.cancelRequest != nil {
		p.cancelRequest()
	}
	p.generation++
	p.thumbnailPending = false
	p.loadingDirectory, p.loadingFile, p.openingTerminal = false, false, false
	p.pendingSelected = ""
	p.installListing(p.allEntries, selected, true)
}

func (p *Provider) searchKey(event experience.Event) bool {
	if event.Keycode == 33 && event.Modifiers == experience.ModControl {
		if !event.Repeat {
			p.searchActive, p.dirty = true, true
		}
		return true
	}
	if !p.searchActive {
		return false
	}
	text := p.text.Handle(event)
	switch event.Keycode {
	case 1:
		p.setQuery("")
		p.searchActive, p.dirty = false, true
	case 28, 96, 15:
		if !event.Repeat {
			p.searchActive, p.dirty = false, true
		}
	case 14:
		if event.Modifiers.Has(experience.ModControl) {
			p.setQuery("")
		} else {
			if p.searchField.Handle(event, text) {
				p.setQuery(p.searchField.Text())
			}
		}
	default:
		if p.searchField.Handle(event, text) {
			p.setQuery(p.searchField.Text())
		}
	}
	p.dirty = true
	return true
}

func (p *Provider) autoRefresh() {
	if p.closed || p.loadingDirectory || p.loadingFile || p.openingTerminal || p.operationPending || p.dialog != nil || p.searchActive || p.now().Before(p.nextRefresh) {
		return
	}
	p.refreshSelection(p.selectionName())
}
