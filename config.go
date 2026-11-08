package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Hotkey — сочетание клавиш. Code — значение KeyboardEvent.code из страницы
// (Digit1, KeyQ, F5 …); пустой Code значит «не назначено».
type Hotkey struct {
	Ctrl  bool   `json:"ctrl"`
	Alt   bool   `json:"alt"`
	Shift bool   `json:"shift"`
	Win   bool   `json:"win"`
	Code  string `json:"code"`
}

func (h Hotkey) Empty() bool { return h.Code == "" }

func (h Hotkey) Same(o Hotkey) bool {
	return !h.Empty() && h == o
}

type Phrase struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Hotkey Hotkey `json:"hotkey"`
}

type Settings struct {
	ToggleHotkey       Hotkey `json:"toggleHotkey"`
	Opacity            int    `json:"opacity"`   // непрозрачность всего окна, 20–100 %
	PasteMode          string `json:"pasteMode"` // "clipboard" (Ctrl+V) или "type"
	CheckUpdates       bool   `json:"checkUpdates"`
	RestoreClipboard   bool   `json:"restoreClipboard"`
	PressEnter         bool   `json:"pressEnter"`
	AlwaysOnTop        bool   `json:"alwaysOnTop"`
	ShowWithoutFocus   bool   `json:"showWithoutFocus"`
	X                  int    `json:"x"`
	Y                  int    `json:"y"`
	Width              int    `json:"width"`
	Height             int    `json:"height"`
	PositionRemembered bool   `json:"positionRemembered"`
}

type Config struct {
	Version  int      `json:"version"`
	Phrases  []Phrase `json:"phrases"`
	Settings Settings `json:"settings"`
}

func defaultConfig() Config {
	texts := []string{
		"Приветствую, сейчас займусь вашим обращением.",
		"Сейчас ознакомлюсь с ситуацией, ожидайте, пожалуйста.",
		"Игрок наказан. Приятной игры!",
		"Не увидел нарушений. Если у вас есть доказательства нарушения игрока - вы можете подать жалобу на форум проекта.",
		"У вас остались ещё вопросы или могу закрывать обращение?",
	}
	cfg := Config{Version: 1}
	for i, t := range texts {
		cfg.Phrases = append(cfg.Phrases, Phrase{
			ID:     fmt.Sprintf("p%d", i+1),
			Text:   t,
			Hotkey: Hotkey{Alt: true, Code: fmt.Sprintf("Digit%d", i+1)},
		})
	}
	cfg.Settings = Settings{
		ToggleHotkey:     Hotkey{Ctrl: true, Alt: true, Code: "KeyB"},
		Opacity:          90,
		PasteMode:        "clipboard",
		CheckUpdates:     true,
		RestoreClipboard: true,
		AlwaysOnTop:      true,
		ShowWithoutFocus: true,
		Width:            400,
		Height:           560,
	}
	return cfg
}

func configDir() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		exe, _ := os.Executable()
		return filepath.Join(filepath.Dir(exe), "data")
	}
	return filepath.Join(base, "T31K Binder")
}

func configPath() string { return filepath.Join(configDir(), "config.json") }

func loadConfig() Config {
	data, err := os.ReadFile(configPath())
	if err != nil {
		return defaultConfig()
	}
	cfg := defaultConfig()
	cfg.Phrases = nil
	if err := json.Unmarshal(data, &cfg); err != nil {
		// Испорченный файл не теряем — откладываем рядом.
		_ = os.Rename(configPath(), configPath()+".broken-"+time.Now().Format("20060102-150405"))
		return defaultConfig()
	}
	cfg.normalize()
	return cfg
}

func (c *Config) normalize() {
	s := &c.Settings
	if s.Opacity == 0 {
		s.Opacity = 90
	}
	s.Opacity = clamp(s.Opacity, 20, 100)
	if s.PasteMode != "type" {
		s.PasteMode = "clipboard"
	}
	if c.Phrases == nil {
		c.Phrases = []Phrase{}
	}
}

func saveConfig(cfg Config) error {
	if err := os.MkdirAll(configDir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := configPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, configPath())
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
