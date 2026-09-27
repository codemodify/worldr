package workspace

import "testing"

func TestNavigatorCommandPaletteTerminalEntersNewAppFromHome(t *testing.T) {
	w, apps := navigatorApplications(t)
	if !w.openCommands() {
		t.Fatal("could not open Search")
	}
	w.commands.entries = []commandEntry{{kind: "terminal"}}
	w.commands.selected = 0
	w.chooseCommand()
	w.Draw(1280, 820)
	if len(apps.launched) != 1 || apps.launched[0] != "terminal" || w.navigation.level != navigationApp || !w.OwnsKeyboard() || w.application.ID != apps.surfaces[len(apps.surfaces)-1].ID {
		t.Fatal("Search terminal launch did not enter and focus its new client")
	}
}

func TestNavigatorCommandPaletteEntersAlreadySelectedProject(t *testing.T) {
	w, _ := navigatorApplications(t)
	if !w.openCommands() {
		t.Fatal("could not open Search")
	}
	w.commands.entries = []commandEntry{{action: Action{Kind: SwitchSpace, Space: 0}}}
	w.commands.selected = 0
	w.chooseCommand()
	w.Draw(1280, 820)
	if w.navigation.level != navigationProject || w.OwnsKeyboard() {
		t.Fatal("selecting the current space from Home did not enter its overview")
	}
}
