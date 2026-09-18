package nativeapps

import (
	"reflect"
	"strings"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/terminal"
)

func TestTaskRecipeValidationIsBoundedAndRejectsExecutableStateTricks(t *testing.T) {
	valid := TaskRecipe{Name: "Unit tests", Command: "go\ttest ./..."}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	invalid := []TaskRecipe{
		{},
		{Name: " leading", Command: "true"},
		{Name: "escape\x1b", Command: "true"},
		{Name: "bidi\u202e", Command: "true"},
		{Name: strings.Repeat("n", maxTaskNameRunes+1), Command: "true"},
		{Name: "newline", Command: "printf ok\nrm -rf elsewhere"},
		{Name: "return", Command: "true\r"},
		{Name: "separator", Command: "true\u2028false"},
		{Name: "nul", Command: "printf \x00"},
		{Name: "large", Command: strings.Repeat("x", maxTaskCommandBytes+1)},
		{Name: "invalid UTF-8", Command: string([]byte{0xff})},
	}
	for _, recipe := range invalid {
		if err := recipe.Validate(); err == nil {
			t.Fatalf("accepted invalid task recipe: %#v", recipe)
		}
	}
	tooMany := make([]TaskRecipe, maxTaskRecipes+1)
	for i := range tooMany {
		tooMany[i] = TaskRecipe{Name: string(rune('A' + i)), Command: "true"}
	}
	if err := validateTaskRecipes(tooMany); err == nil {
		t.Fatal("accepted an unbounded task deck")
	}
	if err := validateTaskRecipes([]TaskRecipe{{Name: "Build", Command: "true"}, {Name: "build", Command: "false"}}); err == nil {
		t.Fatal("accepted ambiguous task names")
	}
	if err := validateTaskRecipes([]TaskRecipe{{Name: "First", Command: "true"}, {Name: "Second", Command: "true"}}); err == nil {
		t.Fatal("accepted duplicate task commands")
	}
}

func TestTaskToolbarRemainsInsideTheMinimumTerminal(t *testing.T) {
	nodes := terminalToolbar(minWidth)
	if len(nodes) != 5 {
		t.Fatalf("task toolbar controls = %d", len(nodes))
	}
	for _, node := range nodes {
		if node.Bounds.Min.X < 0 || node.Bounds.Max.X > minWidth || node.Bounds.Min.X >= node.Bounds.Max.X {
			t.Fatalf("toolbar control escaped minimum terminal: %+v", node)
		}
	}
}

func TestTasksCaptureStageAndExplicitlyExecuteWithoutLeakingKeys(t *testing.T) {
	p, backend := historyProvider(t)
	backend.history.Lines[0] = asciiHistoryLine(100, "$ printf result")
	backend.history.Lines[1] = asciiHistoryLine(101, "result")
	backend.history.Commands = []terminal.CommandBlock{{ID: 7, CommandLine: 100, CommandColumn: 2, OutputLine: 101, Started: true, Finished: true, Status: 0}}
	backend.snapshot.Revision++

	toolKey(t, p, 37, experience.ModControl|experience.ModShift)
	toolKey(t, p, 20, 0)
	want := []TaskRecipe{{Name: "printf result", Command: "printf result"}}
	if got := p.TaskRecipes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("captured tasks = %#v, want %#v", got, want)
	}
	if len(backend.pasted) != 0 || len(backend.input) != 0 {
		t.Fatal("saving a command sent it to the PTY")
	}

	toolKey(t, p, 20, experience.ModControl|experience.ModShift)
	if p.tools.mode != "tasks" || len(p.tools.rows) != 1 || !strings.Contains(p.tools.rows[0].text, "exit 0") {
		t.Fatalf("task deck did not expose captured status: mode=%q rows=%+v", p.tools.mode, p.tools.rows)
	}
	toolKey(t, p, 31, 0)
	if p.tools.mode != "" || len(backend.pasted) != 1 || backend.pasted[0] != "printf result" {
		t.Fatalf("staging was not exact and non-executing: mode=%q paste=%q", p.tools.mode, backend.pasted)
	}
	if !strings.Contains(p.tools.message, "press Enter") || len(backend.input) != 0 {
		t.Fatal("staging leaked an Enter/key event or omitted its review affordance")
	}

	toolKey(t, p, 20, experience.ModControl|experience.ModShift)
	toolKey(t, p, 28, experience.ModControl)
	if p.tools.mode != "" || len(backend.pasted) != 2 || backend.pasted[1] != "printf result" {
		t.Fatalf("explicit run did not send the exact recipe: %#v", backend.pasted)
	}
	if len(backend.input) != 2 || backend.input[0].Keycode != 28 || !backend.input[0].Pressed || backend.input[1].Pressed || backend.input[0].Depressed != 0 {
		t.Fatalf("explicit run did not synthesize one unmodified Enter: %+v", backend.input)
	}

	toolKey(t, p, 30, 0)
	if len(backend.input) != 4 || backend.input[2].Keycode != 30 {
		t.Fatal("ordinary PTY input did not resume after the task deck closed")
	}
}

func TestTaskStatusTracksRunningAndFinishedVersionOfSameCommand(t *testing.T) {
	p, backend := historyProvider(t)
	if err := p.SetTaskRecipes([]TaskRecipe{{Name: "Compile", Command: "go test ./..."}}); err != nil {
		t.Fatal(err)
	}
	backend.history.Lines[0] = asciiHistoryLine(100, "$ go test ./...")
	backend.history.Lines[1] = asciiHistoryLine(101, "running")
	backend.history.Commands = []terminal.CommandBlock{{ID: 9, CommandLine: 100, CommandColumn: 2, OutputLine: 101, Started: true}}
	p.refreshHistory(true)
	if got := p.tools.tasks[0].status; got != "running" {
		t.Fatalf("running task status = %q", got)
	}
	backend.history.Commands[0].Finished = true
	backend.history.Commands[0].Status = 7
	p.refreshHistory(true)
	if got := p.tools.tasks[0].status; got != "exit 7" {
		t.Fatalf("finished task status = %q", got)
	}
}

func TestTaskDeckCopyDeleteAndSemantics(t *testing.T) {
	p, backend := historyProvider(t)
	recipes := []TaskRecipe{{Name: "Build", Command: "go build ./..."}, {Name: "Test", Command: "go test ./..."}}
	if err := p.SetTaskRecipes(recipes); err != nil {
		t.Fatal(err)
	}
	toolKey(t, p, 20, experience.ModControl|experience.ModShift)
	tree := p.Semantics()
	if tree.FocusedID != "row:0" || len(tree.Nodes) != len(recipes)+len(terminalToolbar(p.renderer.image.Rect.Dx())) {
		t.Fatalf("task semantics = %+v", tree)
	}
	toolKey(t, p, 46, experience.ModControl|experience.ModShift)
	if copied, ok := p.TakeCopy(); !ok || copied != recipes[0].Command {
		t.Fatalf("copied task = %q, %v", copied, ok)
	}
	toolKey(t, p, 111, 0)
	if got := p.TaskRecipes(); !reflect.DeepEqual(got, recipes[1:]) {
		t.Fatalf("deleting task produced %#v", got)
	}
	if len(backend.pasted) != 0 || len(backend.input) != 0 {
		t.Fatal("copy/delete executed a task")
	}
}

func TestTerminalStateRoundTripsOnlyInertTaskDefinitions(t *testing.T) {
	f := newManagerFixture(t, Options{})
	directory, path := sessionDirectory(t)
	recipes := []TaskRecipe{{Name: "Checks", Command: "go test ./..."}}
	state := TerminalState{Key: "native:terminal-3", Directory: path, Tasks: recipes}
	if key, err := f.manager.RestoreTerminalState(state, directory); err != nil || key != state.Key {
		t.Fatalf("restore task terminal = %q, %v", key, err)
	}
	if len(f.backends) != 1 || len(f.backends[0].pasted) != 0 || len(f.backends[0].input) != 0 {
		t.Fatal("restoring inert task definitions sent terminal input")
	}
	got := f.manager.SessionTerminals()
	if !reflect.DeepEqual(got, []TerminalState{state}) {
		t.Fatalf("task session state = %#v, want %#v", got, state)
	}
	got[0].Tasks[0].Name = "caller mutation"
	if f.manager.SessionTerminals()[0].Tasks[0].Name != recipes[0].Name {
		t.Fatal("session snapshot shared manager-owned task storage")
	}

	before := len(f.backends)
	bad := TerminalState{Key: "native:terminal-4", Directory: path, Tasks: []TaskRecipe{{Name: "bad", Command: "true\nfalse"}}}
	if _, err := f.manager.RestoreTerminalState(bad, directory); err == nil || len(f.backends) != before {
		t.Fatal("invalid task state started a shell")
	}
}
