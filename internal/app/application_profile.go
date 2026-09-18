package app

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// ApplicationLaunch describes how to start a process. Only its ID associates
// windows with saved placement; arguments and process state are not a document.
type ApplicationLaunch struct {
	ID      string   `json:"id"`
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	X11     bool     `json:"x11,omitempty"`
}

func loadApplicationProfile(path string) ([]ApplicationLaunch, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open application profile: %w", err)
	}
	defer f.Close()
	const limit = 64 << 10
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read application profile: %w", err)
	}
	if len(data) > limit {
		return nil, fmt.Errorf("application profile exceeds %d bytes", limit)
	}
	var profile struct {
		Version      int                 `json:"version"`
		Applications []ApplicationLaunch `json:"applications"`
	}
	d := json.NewDecoder(strings.NewReader(string(data)))
	d.DisallowUnknownFields()
	if err := d.Decode(&profile); err != nil {
		return nil, fmt.Errorf("decode application profile: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("application profile must contain exactly one JSON value")
	}
	if profile.Version != 1 {
		return nil, fmt.Errorf("unsupported application profile version %d", profile.Version)
	}
	if err := validateApplicationLaunches(profile.Applications); err != nil {
		return nil, err
	}
	return profile.Applications, nil
}

func validateApplicationLaunches(launches []ApplicationLaunch) error {
	if len(launches) == 0 || len(launches) > 32 {
		return fmt.Errorf("launch requires between 1 and 32 applications")
	}
	seen := make(map[string]bool, len(launches))
	for _, launch := range launches {
		if len(launch.ID) == 0 || len(launch.ID) > 64 || seen[launch.ID] {
			return fmt.Errorf("application IDs must be unique and between 1 and 64 characters")
		}
		for _, r := range launch.ID {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
				return fmt.Errorf("application ID %q must use letters, digits, hyphens or underscores", launch.ID)
			}
		}
		seen[launch.ID] = true
		if strings.TrimSpace(launch.Command) == "" || strings.IndexByte(launch.Command, 0) >= 0 {
			return fmt.Errorf("application %q needs an executable", launch.ID)
		}
		for _, arg := range launch.Args {
			if strings.IndexByte(arg, 0) >= 0 {
				return fmt.Errorf("application %q argument contains a NUL byte", launch.ID)
			}
		}
	}
	return nil
}

func applicationLaunches(o Options) ([]ApplicationLaunch, error) {
	if len(o.Launches) > 0 {
		if err := validateApplicationLaunches(o.Launches); err != nil {
			return nil, err
		}
		return o.Launches, nil
	}
	names := o.Applications
	if len(names) == 0 && o.Application != "" {
		names = []string{o.Application}
	}
	launches := make([]ApplicationLaunch, len(names)+len(o.X11Applications))
	for i, name := range names {
		launches[i] = ApplicationLaunch{ID: fmt.Sprintf("launch-%d", i+1), Command: name, Args: o.ApplicationArgs}
	}
	for i, name := range o.X11Applications {
		launches[len(names)+i] = ApplicationLaunch{ID: fmt.Sprintf("launch-%d", len(names)+i+1), Command: name, Args: o.ApplicationArgs, X11: true}
	}
	if err := validateApplicationLaunches(launches); err != nil {
		return nil, err
	}
	return launches, nil
}
