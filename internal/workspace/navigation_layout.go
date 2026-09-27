package workspace

import (
	"fmt"
	"math"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
)

// Navigation changes the presentation of existing application slots. No app is
// recreated, reparented into another provider, or resized during a transition.
func (w *Workspace) layoutNavigation() {
	n := w.navigation
	width, height := float32(w.width), float32(w.height)
	u := max(.85, min(width/1280, height/820))
	l := navigationLayout{width: width, height: height, unit: u}
	n.level = w.navigationLevel()
	margin, gap := 24*u, 18*u
	// Keep explicit click targets even on small laptop windows.
	l.back = box{margin, 16 * u, 40 * u, 38 * u}
	l.home = box{margin + 48*u, 16 * u, 72 * u, 38 * u}
	l.overview = box{margin + 128*u, 16 * u, 104 * u, 38 * u}
	l.search = box{width - margin - 268*u, 16 * u, 100 * u, 38 * u}
	l.tools = box{width - margin - 160*u, 16 * u, 80 * u, 38 * u}
	l.motion = box{width - margin - 72*u, 16 * u, 72 * u, 38 * u}
	l.content = box{margin, 160 * u, width - 2*margin, max(100, height-204*u)}
	l.previous = box{margin, 108 * u, 38 * u, 34 * u}
	l.next = box{margin + 46*u, 108 * u, 38 * u, 34 * u}
	l.pagePrevious = box{width - margin - 166*u, height - 38*u, 38 * u, 30 * u}
	l.pageNext = box{width - margin - 42*u, height - 38*u, 38 * u, 30 * u}
	view := w.m.applicationState
	all := make([]navigationAppCard, 0, len(w.applicationSurfaces))
	for slot, p := range view.Layouts {
		if p.Key == "" {
			continue
		}
		for _, s := range w.applicationSurfaces {
			if s.Key == p.Key {
				title := s.Title
				if title == "" {
					title = s.AppID
				}
				if title == "" {
					title = "Application"
				}
				all = append(all, navigationAppCard{slot: slot, id: s.ID, key: s.Key, title: title, active: p.Key == view.Active, minimized: p.Minimized})
				break
			}
		}
	}
	var target [MaxApplicationLayouts]navigationPose
	for i := range target {
		target[i] = navigationPose{bounds: box{margin, 110 * u, 2, 2}}
	}
	place := func(card navigationAppCard, outer box) {
		card.bounds = outer
		area := box{outer.x + 1, outer.y + 28*u, outer.w - 2, max(1, outer.h-28*u-1)}
		aspect := float32(1.6)
		for _, s := range w.applicationSurfaces {
			if s.ID == card.id && s.Texture != nil {
				tw, th := s.Texture.Size()
				if th > 0 {
					aspect = float32(tw) / float32(th)
				}
				if s.ContentAspect > 0 {
					aspect = s.ContentAspect
				}
				break
			}
		}
		card.content = navigationFit(area, aspect)
		target[card.slot] = navigationPose{bounds: card.content, visible: true}
		l.apps = append(l.apps, card)
	}
	page := func(count, perPage int) (int, int) {
		pages := max(1, (count+perPage-1)/perPage)
		effective := max(0, min(n.page, pages-1))
		if count > 0 {
			n.page = effective
		}
		l.page, l.pages = effective, pages
		return effective * perPage, min(count, (effective+1)*perPage)
	}
	if n.level == navigationHome {
		var projects []navigationProjectCard
		for i := range view.Spaces {
			if view.spaceExists(uint8(i)) {
				p := navigationProjectCard{space: uint8(i), name: view.spaceName(uint8(i))}
				for _, a := range all {
					if view.Layouts[a.slot].Space == p.space {
						p.count++
					}
				}
				projects = append(projects, p)
			}
		}
		cols := min(len(projects), max(1, int(l.content.w/(340*u))))
		perPage := cols * 2
		start, end := page(len(projects), perPage)
		rows := max(1, (end-start+cols-1)/cols)
		cw := (l.content.w - float32(cols-1)*gap) / float32(cols)
		ch := (l.content.h - float32(rows-1)*gap) / float32(rows)
		for i := start; i < end; i++ {
			p := projects[i]
			j := i - start
			p.bounds = box{l.content.x + float32(j%cols)*(cw+gap), l.content.y + float32(j/cols)*(ch+gap), cw, ch}
			l.projects = append(l.projects, p)
			var apps []navigationAppCard
			for _, a := range all {
				if view.Layouts[a.slot].Space == p.space {
					apps = append(apps, a)
				}
			}
			// One main preview and two companions reveal each project's contents.
			area := box{p.bounds.x + 14*u, p.bounds.y + 54*u, p.bounds.w - 28*u, p.bounds.h - 70*u}
			if len(apps) > 0 {
				if len(apps) == 1 {
					place(apps[0], area)
				} else {
					main := area
					main.h = area.h*.62 - 6*u
					place(apps[0], main)
					count := min(2, len(apps)-1)
					sw := (area.w - float32(count-1)*10*u) / float32(count)
					for k := range count {
						place(apps[k+1], box{area.x + float32(k)*(sw+10*u), area.y + area.h*.62 + 4*u, sw, area.h*.38 - 4*u})
					}
				}
			}
		}
	} else {
		// The current space stays visible even if the workspace has many spaces.
		var spaces []uint8
		for i := range view.Spaces {
			if view.spaceExists(uint8(i)) {
				spaces = append(spaces, uint8(i))
			}
		}
		capacity := max(1, int((width-margin*2-112*u)/(138*u)))
		active := 0
		for i, id := range spaces {
			if id == view.Space {
				active = i
			}
		}
		tabStart := (active / capacity) * capacity
		for i := tabStart; i < min(len(spaces), tabStart+capacity); i++ {
			id := spaces[i]
			l.tabs = append(l.tabs, navigationSpaceTab{space: id, name: view.spaceName(id), bounds: box{margin + 112*u + float32(i-tabStart)*138*u, 108 * u, 130 * u, 34 * u}, active: id == view.Space})
		}
		var apps []navigationAppCard
		for _, a := range all {
			if view.Layouts[a.slot].Space == view.Space {
				apps = append(apps, a)
			}
		}
		if n.level == navigationProject {
			cols := max(1, int(l.content.w/(320*u)))
			start, end := page(len(apps), cols*2)
			rows := max(1, (end-start+cols-1)/cols)
			cw := (l.content.w - float32(cols-1)*gap) / float32(cols)
			ch := min(330*u, (l.content.h-float32(rows-1)*gap)/float32(rows))
			for i := start; i < end; i++ {
				j := i - start
				place(apps[i], box{l.content.x + float32(j%cols)*(cw+gap), l.content.y + float32(j/cols)*(ch+gap), cw, ch})
			}
		} else {
			strip := min(168*u, l.content.w*.22)
			var siblings []navigationAppCard
			for _, a := range apps {
				if a.active {
					place(a, box{l.content.x + strip + gap, l.content.y, l.content.w - strip - gap, l.content.h})
				} else {
					siblings = append(siblings, a)
				}
			}
			for _, a := range all {
				if view.Layouts[a.slot].Space != view.Space {
					a.title = view.spaceName(view.Layouts[a.slot].Space) + " · " + a.title
					siblings = append(siblings, a)
				}
			}
			perPage := max(1, int(l.content.h/(126*u)))
			start, end := page(len(siblings), perPage)
			for i := start; i < end; i++ {
				place(siblings[i], box{l.content.x, l.content.y + float32(i-start)*126*u, strip, 114 * u})
			}
		}
	}
	// Outgoing apps remain labelled while their content returns to its anchor.
	for _, a := range all {
		if !target[a.slot].visible {
			l.apps = append(l.apps, a)
		}
	}
	tools := []navigationTool{{id: "launch:note", label: "New note"}, {id: "launch:files", label: "Open files"}, {id: "launch:terminal", label: "Open terminal"}, {id: "organize", label: "Find apps / create or move spaces"}, {id: "minimize", label: "Minimize app", disabled: n.level != navigationApp}, {id: "close", label: "Close app", disabled: n.level != navigationApp}, {id: "home", label: "Return to projects"}}
	if _, ok := w.applications.(experience.ApplicationLauncher); !ok {
		for i := 0; i < 3; i++ {
			tools[i].disabled = true
		}
	}
	if _, ok := w.applications.(experience.ApplicationCloser); !ok {
		tools[5].disabled = true
	}
	l.toolPanel = box{width - margin - 320*u, 64 * u, 320 * u, float32(len(tools))*39*u + 58*u}
	for i, t := range tools {
		t.bounds = box{l.toolPanel.x + 10*u, l.toolPanel.y + 46*u + float32(i)*39*u, l.toolPanel.w - 20*u, 37 * u}
		l.toolItems = append(l.toolItems, t)
	}
	n.layout = l
	if !n.motionEnabled || n.lastWidth != w.width || n.lastHeight != w.height {
		n.motion.snap(target)
	} else {
		n.motion.retarget(target)
	}
	n.lastWidth, n.lastHeight = w.width, w.height
}

func navigationFit(area box, aspect float32) box {
	width, height := area.w, area.h
	if width/height > aspect {
		width = height * aspect
	} else {
		height = width / aspect
	}
	return box{area.x + (area.w-width)/2, area.y + (area.h-height)/2, max(1, width), max(1, height)}
}

func (w *Workspace) syncNavigationScene() {
	n := w.navigation
	w.camera = scene.Camera{Eye: scene.Vec3{Z: 10}, Up: scene.Vec3{Y: 1}, FOV: .69, Near: .1, Far: 100}
	for _, id := range []scene.NodeID{w.stageNode, w.accentNode, w.hologramNode, w.panelNode, w.nodes[0], w.nodes[1], w.nodes[2]} {
		if node := w.scene.Node(id); node != nil {
			node.Hidden = true
		}
	}
	poses := n.motion.poses()
	for _, s := range w.applicationSurfaces {
		node := w.scene.Node(w.applicationNodes[s.ID])
		if node == nil {
			continue
		}
		slot := w.m.applicationState.index(s.Key)
		if slot < 0 {
			node.Hidden = true
			continue
		}
		p := poses[slot]
		node.Hidden = !p.visible
		node.Unpickable = n.level != navigationApp || s.Key != w.m.applicationState.Active
		node.Surface = s.Texture
		node.Color = scene.ColorHex(0xffffff, 1)
		node.Glow = [3]float32{}
		depth := float32(10)
		z := float32(0)
		if s.Key == w.m.applicationState.Active {
			depth = 9.98
			z = .02
		}
		factor := 2 * depth * float32(math.Tan(.69/2)) / max(1, float32(w.height))
		center := scene.Vec3{X: (p.bounds.x + p.bounds.w/2 - float32(w.width)/2) * factor, Y: -(p.bounds.y + p.bounds.h/2 - float32(w.height)/2) * factor, Z: z}
		node.Transform = applicationPlane(center, scene.Vec3{X: 1}, scene.Vec3{Y: 1}, scene.Vec3{Z: 1}, p.bounds.w*factor, p.bounds.h*factor)
		w.placeSpatialApplication(s, p.bounds.w*factor, p.bounds.h*factor)
		if mount := w.applicationSpatial[s.ID]; mount != nil {
			w.scene.Node(mount.root).Hidden = false
		}
		for _, id := range []scene.NodeID{w.applicationFrames[s.ID], w.applicationDragHandles[s.ID], w.applicationWindowControls[s.ID], w.applicationResizeHandles[s.ID]} {
			if frame := w.scene.Node(id); frame != nil {
				frame.Hidden = true
			}
		}
	}
}

func (w *Workspace) drawNavigation(width, height int) render.Frame {
	w.beginShapedLabels()
	w.syncApplications()
	if (w.width != width || w.height != height) && w.applicationCapturedID != 0 {
		w.clearApplicationFocus()
	}
	w.layout(width, height)
	w.syncScene()
	w.canvas.Reset(width, height)
	w.canvas.SetLinearColor(true)
	w.drawNavigationBackdrop()
	w.fitApplicationShadow()
	w.scene.Draw(w.canvas, w.camera, w.viewport)
	w.drawNavigationSpatialLabels()
	w.drawNavigationChrome()
	return w.commandFrame(w.shapedFrame(w.canvas.Frame()))
}

// Native 3D labels use the same presentation and occlusion as their retained
// objects, while staying inside the preview and below Navigator's tools.
func (w *Workspace) drawNavigationSpatialLabels() {
	n := w.navigation
	poses := n.motion.poses()
	for _, surface := range w.applicationSurfaces {
		slot := w.m.applicationState.index(surface.Key)
		if slot < 0 || !poses[slot].visible || surface.Spatial == nil {
			continue
		}
		b := poses[slot].bounds
		transform := w.spatialApplicationTransform(surface)
		for _, label := range surface.Spatial.Labels {
			x, y, z, visible := w.camera.Project(transform.TransformPoint(label.Position), w.viewport)
			if !visible || !b.contains(x, y) {
				continue
			}
			if hit, ok := w.scene.Pick(w.camera, w.viewport, x, y); ok && hit.Depth+.0001 < z {
				continue
			}
			size := max(8, min(11*n.layout.unit, b.w/36))
			bounds := box{x, y, min(220*n.layout.unit, b.x+b.w-x), size * 1.4}
			if bounds.y+bounds.h > b.y+b.h || navigationVisualIntersects(bounds, w.navigationVisualToolBounds()) {
				continue
			}
			color := label.Color
			if color == (scene.Color{}) {
				color = scene.ColorHex(0xc4efff, 1)
			}
			w.shapedText(x, y, size, bounds.w, label.Text, color)
		}
	}
}

func (w *Workspace) updateNavigation(dt time.Duration) {
	w.layout(w.width, w.height)
	n := w.navigation
	if w.applicationCapturedID == 0 {
		n.motion.update(dt)
	}
	if dt > 0 {
		step := float32(dt.Seconds() / .18)
		if n.toolsOpen {
			n.toolProgress = min(1, n.toolProgress+step)
		} else {
			n.toolProgress = max(0, n.toolProgress-step)
		}
		if !n.motionEnabled {
			if n.toolsOpen {
				n.toolProgress = 1
			} else {
				n.toolProgress = 0
			}
		}
		if w.applicationNoticeRemaining > 0 {
			w.applicationNoticeRemaining -= dt
			if w.applicationNoticeRemaining <= 0 {
				w.applicationNotice = ""
			}
		}
	}
}

func (w *Workspace) demoNavigation(elapsed time.Duration) {
	step := int(elapsed / (2 * time.Second))
	if step == w.navigation.demoStep {
		return
	}
	w.navigation.demoStep = step
	switch step % 5 {
	case 0:
		w.navigationHome()
	case 1:
		w.navigationProject(w.m.applicationState.Space)
	case 2:
		for _, s := range w.applicationSurfaces {
			if w.inCurrentSpace(s) {
				_ = w.ActivateApplication(s.Key)
				break
			}
		}
	case 3:
		w.navigation.toolsOpen = true
		w.clearApplicationFocus()
	case 4:
		w.navigation.toolsOpen = false
		w.navigationProject(w.m.applicationState.Space)
	}
}

func navigationID(prefix string, i int) string { return fmt.Sprintf("%s:%d", prefix, i) }
