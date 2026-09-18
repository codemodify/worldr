package projectapp

// OpenIntent limits Files to one kind of native document while keeping
// folders available for navigation. OpenAny restores the normal browser.
type OpenIntent uint8

const (
	OpenAny OpenIntent = iota
	OpenPhoto
	OpenVideo
	OpenModel
	OpenDataset
)

func (intent OpenIntent) valid() bool {
	return intent >= OpenAny && intent <= OpenDataset
}

func (intent OpenIntent) accepts(path string) bool {
	switch intent {
	case OpenPhoto:
		return IsPhotoPath(path)
	case OpenVideo:
		return videoPath(path)
	case OpenModel:
		return IsModelPath(path)
	case OpenDataset:
		return IsDatasetPath(path)
	default:
		return true
	}
}

func (intent OpenIntent) title() string {
	switch intent {
	case OpenPhoto:
		return "CHOOSE PHOTO"
	case OpenVideo:
		return "CHOOSE VIDEO"
	case OpenModel:
		return "CHOOSE 3D MODEL"
	case OpenDataset:
		return "CHOOSE RESEARCH DATA"
	default:
		return ""
	}
}

func (intent OpenIntent) instruction() string {
	switch intent {
	case OpenPhoto:
		return "Choose a JPG, PNG, WebP, GIF or BMP photo · folders remain available · Esc shows all files"
	case OpenVideo:
		return "Choose a video to play · folders remain available · Esc shows all files"
	case OpenModel:
		return "Choose an OBJ, STL or .worldr-model.json model · folders remain available · Esc shows all files"
	case OpenDataset:
		return "Choose a CSV, TSV or .worldr-data.json dataset · folders remain available · Esc shows all files"
	default:
		return ""
	}
}

func (intent OpenIntent) emptyMessage() string {
	switch intent {
	case OpenPhoto:
		return "No supported photos in this folder."
	case OpenVideo:
		return "No supported videos in this folder."
	case OpenModel:
		return "No supported 3D models in this folder."
	case OpenDataset:
		return "No supported research datasets in this folder."
	default:
		return "This directory is empty."
	}
}

// SetOpenIntent enters a bounded choose-file mode. It can be called again on
// an already-visible Files surface, which is how the workspace dock focuses a
// single browser instead of opening duplicate picker windows.
func (p *Provider) SetOpenIntent(intent OpenIntent) {
	if p.closed || !intent.valid() {
		return
	}
	p.cancelFieldPaste()
	defer p.syncFieldContext()
	selected := p.selectionName()
	p.openIntent = intent
	p.query, p.searchActive = "", false
	p.searchField.Set("")
	p.notice = ""
	// Keep an initial or directory listing in flight: its result will apply the
	// new intent. A preview/open request is no longer relevant to this chooser.
	if p.loadingDirectory && p.allEntries == nil {
		p.dirty = true
		return
	}
	if p.loadingFile || p.openingTerminal {
		if p.cancelRequest != nil {
			p.cancelRequest()
		}
		p.generation++
		p.loadingFile, p.openingTerminal, p.thumbnailPending = false, false, false
		p.previewPath = ""
	}
	p.installListing(p.allEntries, selected, true)
	p.dirty = true
}
