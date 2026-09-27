// Command worldr-terminal runs a real shell in a standalone glass GPU window.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/codemodify/worldr/internal/glass"
	"github.com/codemodify/worldr/internal/terminal"
	kit "github.com/codemodify/worldr/sdk/app/v1"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "worldr-terminal:", err)
		os.Exit(1)
	}
}
func run() error {
	var o kit.Options
	var command, directory, config, theme string
	var opacity float64
	var motion bool
	flag.IntVar(&o.Width, "width", 1180, "initial logical window width")
	flag.IntVar(&o.Height, "height", 760, "initial logical window height")
	flag.IntVar(&o.FPS, "fps", 60, "maximum frame/update rate")
	flag.BoolVar(&o.Headless, "headless", false, "render offscreen for verification")
	flag.IntVar(&o.Frames, "frames", 0, "stop after this many submitted frames")
	flag.DurationVar(&o.Duration, "duration", 0, "stop after a duration, e.g. 2s")
	flag.StringVar(&o.Snapshot, "snapshot", "", "write a PNG with real transparency on exit")
	flag.StringVar(&command, "command", "", "shell or executable (defaults to $SHELL); arguments follow --")
	flag.StringVar(&directory, "directory", "", "initial working directory")
	flag.StringVar(&config, "config", "", "appearance settings path; default is the user configuration directory")
	flag.StringVar(&theme, "theme", "cyan", "cyan|amber|iris")
	flag.Float64Var(&opacity, "opacity", .82, "glass opacity, 0.35 to 1")
	flag.BoolVar(&motion, "motion", true, "animate appearance changes and transitions")
	flag.Parse()
	if o.Width < 640 || o.Height < 420 {
		return errors.New("initial window must be at least 640x420")
	}
	explicit := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	if config == "" && !explicit["config"] && !o.Headless {
		base, err := os.UserConfigDir()
		if err != nil {
			return err
		}
		config = filepath.Join(base, "worldr", "terminal.json")
	}
	prefs := glass.DefaultPreferences()
	if config != "" {
		data, err := os.ReadFile(config)
		if err == nil {
			var saved glass.Preferences
			if err = json.Unmarshal(data, &saved); err != nil || !saved.Valid() {
				return fmt.Errorf("invalid appearance settings in %s", config)
			}
			prefs = saved
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if explicit["theme"] {
		switch theme {
		case "cyan":
			prefs.Palette = 0
		case "amber":
			prefs.Palette = 1
		case "iris":
			prefs.Palette = 2
		default:
			return fmt.Errorf("unknown theme %q", theme)
		}
	}
	if explicit["opacity"] {
		prefs.Opacity = float32(opacity)
	}
	if explicit["motion"] {
		prefs.Motion = motion
	}
	if !prefs.Valid() {
		return errors.New("opacity must be 0.35..1 and appearance values must be finite")
	}
	if command == "" {
		command = os.Getenv("SHELL")
		if command == "" {
			command = "/bin/sh"
		}
	}
	t := terminal.Options{Command: command, Args: flag.Args(), Cols: 110, Rows: 28, Scrollback: 10000}
	if directory != "" {
		f, err := os.Open(directory)
		if err != nil {
			return err
		}
		defer f.Close()
		t.Directory = f
	}
	a, err := glass.New(t, prefs)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	o.Title = "WorldR Terminal"
	o.Transparent = true
	o.ClientDecorated = true
	if o.Headless && o.Duration == 0 && o.Frames == 0 {
		o.Duration = time.Second
	}
	fmt.Fprintln(os.Stdout, "WorldR Terminal · standalone GPU window · "+command)
	err = kit.Run(ctx, o, a)
	if err != nil {
		return err
	}
	if config != "" {
		return savePreferences(config, a.Preferences())
	}
	return nil
}
func savePreferences(path string, prefs glass.Preferences) error {
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".terminal-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
