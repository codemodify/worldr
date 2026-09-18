package projectapp

import (
	"image"
	"path/filepath"
)

const thumbnailSide = 18
const maxThumbnails = 256 // Covers all visible rows even at the maximum surface height.

type thumbnailRecord struct {
	stamp fileStamp
	image *image.RGBA // nil remembers a failed bounded decode until the file changes.
}

func (p *Provider) retryFailedThumbnails() {
	order := p.thumbnailOrder[:0]
	for _, path := range p.thumbnailOrder {
		if p.thumbnails[path].image == nil {
			delete(p.thumbnails, path)
		} else {
			order = append(order, path)
		}
	}
	p.thumbnailOrder = order
}

func (p *Provider) queueThumbnail() {
	if !p.thumbnailsEnabled || p.closed || p.thumbnailPending || p.loadingDirectory || p.loadingFile || p.openingTerminal || p.operationPending || p.dialog != nil || p.searchActive {
		return
	}
	for row := 0; row < p.rows() && p.listTop+row < len(p.entries); row++ {
		item := p.entries[p.listTop+row]
		if item.kind != fileEntry || !IsPhotoPath(item.name) {
			continue
		}
		path := filepath.Join(p.directory, item.name)
		if cached, ok := p.thumbnails[path]; ok && cached.stamp == item.stamp {
			continue
		}
		p.submit(request{path: path, thumbnail: true, thumbnailStamp: item.stamp})
		return
	}
}

func (p *Provider) thumbnailResult(res *result) {
	p.thumbnailPending = false
	if p.thumbnails == nil {
		p.thumbnails = make(map[string]thumbnailRecord)
	}
	if _, ok := p.thumbnails[res.path]; !ok {
		if len(p.thumbnailOrder) == maxThumbnails {
			delete(p.thumbnails, p.thumbnailOrder[0])
			p.thumbnailOrder = p.thumbnailOrder[1:]
		}
		p.thumbnailOrder = append(p.thumbnailOrder, res.path)
	}
	p.thumbnails[res.path] = thumbnailRecord{stamp: res.thumbnailStamp, image: res.thumbnailImage}
	// Thumbnail failures leave the useful Photo file hint intact. Explicit Open
	// still reports the viewer's full decode error, rather than a thumbnail limit.
}

func (p *Provider) thumbnailFor(item entry) *image.RGBA {
	record := p.thumbnails[filepath.Join(p.directory, item.name)]
	if record.stamp != item.stamp {
		return nil
	}
	return record.image
}
