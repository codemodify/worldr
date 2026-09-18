package nativeapps

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/terminal"
)

const (
	maxTaskRecipes      = 32
	maxTaskNameRunes    = 80
	maxTaskCommandBytes = 4096
	maxTaskRecipeBytes  = 64 << 10
	taskFoldMask        = uint64(1) << 62
)

// TaskRecipe is an inert, reusable shell command definition. It deliberately
// contains no run-on-open flag, process identity, environment, or saved status.
// A restored recipe cannot start work until a person stages or runs it again.
type TaskRecipe struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

func hiddenTaskControl(ch rune) bool {
	return unicode.IsControl(ch) || unicode.In(ch, unicode.Cf, unicode.Zl, unicode.Zp)
}

// Validate bounds a recipe and rejects terminal control sequences and command
// separators hidden in line breaks. Tabs remain valid shell whitespace.
func (r TaskRecipe) Validate() error {
	if !utf8.ValidString(r.Name) || !utf8.ValidString(r.Command) {
		return fmt.Errorf("task recipe must be valid UTF-8")
	}
	if r.Name == "" || r.Name != strings.TrimSpace(r.Name) {
		return fmt.Errorf("task recipe name must be nonempty and trimmed")
	}
	if utf8.RuneCountInString(r.Name) > maxTaskNameRunes {
		return fmt.Errorf("task recipe name exceeds %d characters", maxTaskNameRunes)
	}
	for _, ch := range r.Name {
		if hiddenTaskControl(ch) {
			return fmt.Errorf("task recipe name contains a control character")
		}
	}
	if r.Command == "" || r.Command != strings.TrimSpace(r.Command) {
		return fmt.Errorf("task command must be nonempty and trimmed")
	}
	if len(r.Command) > maxTaskCommandBytes {
		return fmt.Errorf("task command exceeds %d bytes", maxTaskCommandBytes)
	}
	for _, ch := range r.Command {
		if hiddenTaskControl(ch) && ch != '\t' {
			return fmt.Errorf("task command must be a single printable line")
		}
	}
	return nil
}

func validateTaskRecipes(recipes []TaskRecipe) error {
	if len(recipes) > maxTaskRecipes {
		return fmt.Errorf("at most %d terminal task recipes can be saved", maxTaskRecipes)
	}
	total := 0
	names := make(map[string]bool, len(recipes))
	commands := make(map[string]bool, len(recipes))
	for i, recipe := range recipes {
		if err := recipe.Validate(); err != nil {
			return fmt.Errorf("task recipe %d: %w", i+1, err)
		}
		total += len(recipe.Name) + len(recipe.Command)
		if total > maxTaskRecipeBytes {
			return fmt.Errorf("terminal task recipes exceed %d bytes", maxTaskRecipeBytes)
		}
		name := strings.ToLower(recipe.Name)
		if names[name] {
			return fmt.Errorf("duplicate terminal task name %q", recipe.Name)
		}
		names[name] = true
		if commands[recipe.Command] {
			return fmt.Errorf("duplicate terminal task command %q", recipe.Command)
		}
		commands[recipe.Command] = true
	}
	return nil
}

type terminalTask struct {
	TaskRecipe
	id          uint64
	lastCommand uint64
	status      string
}

// TaskRecipes returns a detached copy suitable for the session manifest. Only
// inert definitions leave the provider; live status is intentionally omitted.
func (p *Provider) TaskRecipes() []TaskRecipe {
	if p.closed || len(p.tools.tasks) == 0 {
		return nil
	}
	result := make([]TaskRecipe, len(p.tools.tasks))
	for i := range p.tools.tasks {
		result[i] = p.tools.tasks[i].TaskRecipe
	}
	return result
}

// SetTaskRecipes replaces inert definitions without staging or executing any
// command. It is used after a fresh shell has been restored for a saved slot.
func (p *Provider) SetTaskRecipes(recipes []TaskRecipe) error {
	if p.closed {
		return terminal.ErrClosed
	}
	if err := validateTaskRecipes(recipes); err != nil {
		return err
	}
	tasks := make([]terminalTask, len(recipes))
	p.tools.nextTask = 0
	for i, recipe := range recipes {
		p.tools.nextTask++
		tasks[i] = terminalTask{TaskRecipe: recipe, id: p.tools.nextTask, status: "never"}
	}
	for _, task := range p.tools.tasks {
		delete(p.tools.expanded, task.id|taskFoldMask)
	}
	p.tools.tasks = tasks
	p.tools.rowsDirty = true
	return nil
}

func (p *Provider) addTaskRecipe(recipe TaskRecipe) error {
	if err := recipe.Validate(); err != nil {
		return err
	}
	if len(p.tools.tasks) == maxTaskRecipes {
		return fmt.Errorf("at most %d terminal task recipes can be saved", maxTaskRecipes)
	}
	total := len(recipe.Name) + len(recipe.Command)
	for _, task := range p.tools.tasks {
		total += len(task.Name) + len(task.Command)
		if strings.EqualFold(task.Name, recipe.Name) {
			return fmt.Errorf("a task named %q already exists", recipe.Name)
		}
		if task.Command == recipe.Command {
			return fmt.Errorf("that command is already saved as %q", task.Name)
		}
	}
	if total > maxTaskRecipeBytes {
		return fmt.Errorf("terminal task recipes exceed %d bytes", maxTaskRecipeBytes)
	}
	if p.tools.nextTask >= taskFoldMask-1 {
		return fmt.Errorf("terminal task identities exhausted")
	}
	p.tools.nextTask++
	p.tools.tasks = append(p.tools.tasks, terminalTask{TaskRecipe: recipe, id: p.tools.nextTask, status: "never"})
	p.tools.rowsDirty = true
	return nil
}

func commandSource(h terminal.History, command terminal.CommandBlock) string {
	if !command.Started {
		return ""
	}
	return strings.TrimSpace(textRange(h, command.CommandLine, command.CommandColumn, command.OutputLine, command.OutputColumn, maxTaskCommandBytes+1))
}

func taskName(command string) string {
	name := strings.Join(strings.Fields(command), " ")
	return clippedText(name, maxTaskNameRunes)
}

func (p *Provider) saveSelectedCommandTask() {
	u := &p.tools
	if u.selected < 0 || u.selected >= len(u.rows) || u.rows[u.selected].kind != "command" {
		u.message = "Select a command header or output row first"
		return
	}
	id := u.rows[u.selected].id
	for _, command := range u.history.Commands {
		if command.ID != id {
			continue
		}
		source := commandSource(u.history, command)
		recipe := TaskRecipe{Name: taskName(source), Command: source}
		if err := p.addTaskRecipe(recipe); err != nil {
			u.message = err.Error()
			return
		}
		u.message = "Task saved — Ctrl+Shift+T opens Tasks"
		return
	}
	u.message = "That command is no longer retained"
}

func (p *Provider) selectedTask() *terminalTask {
	u := &p.tools
	if u.selected < 0 || u.selected >= len(u.rows) || u.rows[u.selected].kind != "task" {
		return nil
	}
	id := u.rows[u.selected].id
	for i := range u.tasks {
		if u.tasks[i].id == id {
			return &u.tasks[i]
		}
	}
	return nil
}

func (p *Provider) dispatchSelectedTask(execute bool) {
	task := p.selectedTask()
	if task == nil {
		p.tools.message = "Select a saved task first"
		return
	}
	if p.snapshot.Exited {
		p.tools.message = "The shell has exited; this task was not sent"
		return
	}
	command := task.Command
	if execute {
		task.status = "sent"
	} else {
		task.status = "staged"
	}
	p.setTool("")
	if err := p.terminal.Paste(command); err != nil {
		p.remember(err)
		return
	}
	if execute {
		// Use an actual Enter after the bounded paste. A newline inside a
		// bracketed-paste transaction is intentionally editable in many shells
		// and therefore is not a reliable explicit execution gesture.
		for _, pressed := range []bool{true, false} {
			if err := p.terminal.Input(experience.Event{Kind: experience.KeyInput, Keycode: 28, Pressed: pressed}); err != nil {
				p.remember(err)
				return
			}
		}
		p.tools.message = "Task explicitly sent; Runs shows status when shell integration is enabled"
	} else {
		p.tools.message = "Task staged for review — press Enter in the shell to execute"
	}
}

func (p *Provider) refreshTaskStatuses(history terminal.History) {
	changed := false
	for i := range p.tools.tasks {
		task := &p.tools.tasks[i]
		for _, command := range history.Commands {
			if command.ID < task.lastCommand || commandSource(history, command) != task.Command {
				continue
			}
			status := "done"
			switch {
			case !command.Finished:
				status = "running"
			case command.Status >= 0:
				status = fmt.Sprintf("exit %d", command.Status)
			}
			if command.ID > task.lastCommand || task.status != status {
				task.lastCommand = command.ID
				task.status = status
				changed = true
			}
		}
	}
	if changed {
		p.tools.rowsDirty = true
	}
}
