package app

import (
	"fmt"
	"os"

	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

func loadSkin(value string) (skin.Skin, error) {
	if preset, err := skin.Builtin(value); err == nil {
		return preset, nil
	}
	f, err := os.Open(value)
	if err != nil {
		return skin.Skin{}, fmt.Errorf("skin %q: use a preset ID or a readable JSON package: %w", value, err)
	}
	defer f.Close()
	selected, err := skin.Decode(f)
	if err != nil {
		return skin.Skin{}, fmt.Errorf("skin %q: %w", value, err)
	}
	return selected, nil
}

func applyStartupSkin(work any, value string) error {
	if value == "" {
		return nil
	}
	setter, ok := work.(interface{ SetSkin(skin.Skin) error })
	if !ok {
		return fmt.Errorf("this experience does not support skin packages")
	}
	selected, err := loadSkin(value)
	if err != nil {
		return err
	}
	return setter.SetSkin(selected)
}
