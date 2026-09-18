package projectapp

import (
	"fmt"
	"image"

	"github.com/codemodify/worldr/internal/nativeui"
)

// Semantics publishes the visible Files controls in application-pixel space.
// File contents and preview glyphs remain documents rather than thousands of
// synthetic accessibility nodes.
func (p *Provider) Semantics(id uint64) nativeui.SemanticTree {
	if p.closed || id != 1 || p.renderer == nil || p.renderer.image == nil {
		return nativeui.SemanticTree{}
	}
	if p.dialog != nil {
		return p.dialogSemantics()
	}
	busy := p.loadingDirectory || p.loadingFile || p.openingTerminal || p.operationPending
	selected := p.selected >= 0 && p.selected < len(p.entries)
	nodes := make([]nativeui.Node, 0, len(toolbarButtons)+len(operationButtons)+p.rows()+2)
	if title := p.openIntent.title(); title != "" {
		nodes = append(nodes, nativeui.Node{ID: "open-intent", Role: nativeui.RoleLabel, Label: title, Description: p.openIntent.instruction(), Bounds: image.Rect(14, 8, p.renderer.image.Rect.Dx()-14, 34)})
	}
	for index, button := range toolbarButtons {
		disabled := false
		switch index {
		case 0:
			disabled = p.directory == "" || busy
		case 1:
			disabled = p.operationPending
		case 2:
			disabled = !selected || busy
		case 4:
			disabled = p.openingTerminal || p.loadingDirectory || p.operationPending
		case 5:
			disabled = p.operationPending
		}
		nodes = append(nodes, nativeui.Node{ID: fmt.Sprintf("toolbar:%d", index), Role: nativeui.RoleButton, Label: button.label, Bounds: button.rect, Disabled: disabled})
	}
	for index, button := range operationButtons {
		action, label := p.operationButton(button.action)
		if label == "" {
			label = button.label
		}
		disabled := busy || action == undoFileAction && len(p.fileHistory) == 0 || action != createFolder && action != undoFileAction && !selected
		nodes = append(nodes, nativeui.Node{ID: fmt.Sprintf("operation:%d", index), Role: nativeui.RoleButton, Label: label, Bounds: button.rect, Disabled: disabled})
	}
	focused := ""
	if p.searchActive || p.query != "" {
		nodes = append(nodes, nativeui.Node{ID: "search-field", Role: nativeui.RoleTextField, Label: "Filter files", Value: p.searchField.Text(), Bounds: searchFieldRect(p.renderer.image.Rect.Dx()), Disabled: p.operationPending})
		if p.focused && p.searchActive {
			focused = "search-field"
		}
	}
	for row := 0; row < p.rows(); row++ {
		index := p.listTop + row
		if index >= len(p.entries) {
			break
		}
		entry := p.entries[index]
		description := "File"
		switch entry.kind {
		case directoryEntry:
			description = "Folder"
		case symlinkEntry:
			description = "Symbolic link"
		case specialEntry:
			description = "Special file"
		}
		id := fmt.Sprintf("entry:%d", index)
		nodes = append(nodes, nativeui.Node{ID: id, Role: nativeui.RoleMenuItem, Label: safeLabel(entry.name), Description: description,
			Bounds: image.Rect(0, contentTop+row*rowHeight, p.renderer.split(), contentTop+(row+1)*rowHeight), Selected: index == p.selected})
		if p.focused && !p.previewActive && index == p.selected && focused == "" {
			focused = id
		}
	}
	return nativeui.SemanticTree{Nodes: nodes, FocusedID: focused}
}

func (p *Provider) dialogSemantics() nativeui.SemanticTree {
	rect := operationDialogRect(p.renderer.image.Rect.Dx(), p.renderer.image.Rect.Dy())
	confirm := image.Rect(rect.Min.X+18, rect.Max.Y-46, rect.Min.X+130, rect.Max.Y-16)
	cancel := image.Rect(rect.Min.X+140, rect.Max.Y-46, rect.Min.X+250, rect.Max.Y-16)
	nodes := []nativeui.Node{
		{ID: "dialog-title", Role: nativeui.RoleLabel, Label: p.dialog.title, Bounds: image.Rect(rect.Min.X+18, rect.Min.Y+14, rect.Max.X-18, rect.Min.Y+44)},
		{ID: "dialog-confirm", Role: nativeui.RoleButton, Label: "Confirm", Bounds: confirm, Disabled: p.operationPending},
		{ID: "dialog-cancel", Role: nativeui.RoleButton, Label: "Cancel", Bounds: cancel, Disabled: p.operationPending},
	}
	focused := "dialog-confirm"
	if p.dialog.operation.action != trashItem && p.dialog.operation.action != restoreTrashItem {
		field := image.Rect(rect.Min.X+18, rect.Min.Y+54, rect.Max.X-18, rect.Min.Y+88)
		nodes = append(nodes, nativeui.Node{ID: "dialog-field", Role: nativeui.RoleTextField, Label: p.dialog.title, Value: p.dialog.field.Text(), Bounds: field, Disabled: p.operationPending})
		focused = "dialog-field"
	}
	if !p.focused {
		focused = ""
	}
	return nativeui.SemanticTree{Nodes: nodes, FocusedID: focused}
}
