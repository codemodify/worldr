package app

import (
	"fmt"
	"image"
	"io"
	"sort"

	"github.com/codemodify/worldr/internal/platform/linux/native"
)

type directOutput struct {
	connector native.DRMOutput
	bounds    image.Rectangle
}

// Keep connector order stable across inventory reorderings. Explicit --output
// order controls desktop placement; automatic output order uses connector IDs.
func layoutDirectOutputs(available []native.DRMOutput, selected []uint32, diagnostic bool) ([]directOutput, error) {
	byID := map[uint32]native.DRMOutput{}
	for _, out := range available {
		if out.Connected && out.Width > 0 && out.Height > 0 {
			byID[out.ID] = out
		}
	}
	order := append([]uint32(nil), selected...)
	if len(order) == 0 {
		for id := range byID {
			order = append(order, id)
		}
		sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	}
	if diagnostic && len(selected) == 0 && len(order) > 1 {
		order = order[:1]
	}
	var outputs []directOutput
	x, y, rowHeight := 0, 0, 0
	for _, id := range order {
		out, ok := byID[id]
		if !ok {
			continue
		} // A selected monitor may be unplugged.
		width, height := int(out.Width), int(out.Height)
		if !validExtent(width, height) {
			return nil, fmt.Errorf("connector %d has unsupported mode %dx%d", id, width, height)
		}
		if x+width > 7680 {
			x = 0
			y += rowHeight
			rowHeight = 0
		}
		if y+height > 4320 {
			return nil, fmt.Errorf("connected monitors exceed the 7680x4320 desktop; select fewer with --output")
		}
		outputs = append(outputs, directOutput{out, image.Rect(x, y, x+width, y+height)})
		x += width
		rowHeight = max(rowHeight, height)
	}
	if len(outputs) > native.MaxOutputs {
		return nil, fmt.Errorf("at most %d outputs are supported", native.MaxOutputs)
	}
	if diagnostic && len(outputs) > 1 {
		return nil, fmt.Errorf("diagnostic DRM supports one output; use vk-display for several")
	}
	return outputs, nil
}
func directOutputSignature(outputs []directOutput) string { return fmt.Sprintf("%v", outputs) }
func listOutputs(out io.Writer, card string) error {
	outputs, err := native.ListDRMOutputs(card)
	if err != nil {
		return err
	}
	for _, output := range outputs {
		status := "disconnected"
		if output.Connected {
			status = "connected"
		}
		if _, err := fmt.Fprintf(out, "%s · %s · connector %d · %s · %dx%d\n", output.Card, output.Name, output.ID, status, output.Width, output.Height); err != nil {
			return err
		}
	}
	if len(outputs) == 0 {
		_, err = fmt.Fprintln(out, "No DRM connectors found.")
	}
	return err
}
