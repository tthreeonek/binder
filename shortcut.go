package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"golang.org/x/sys/windows/registry"
)

const shortcutName = "T31K Binder.lnk"

// desktopShortcutPath — ярлык на рабочем столе. Путь к рабочему столу берём
// из реестра: он учитывает перенос рабочего стола в OneDrive.
func desktopShortcutPath() string {
	dir := ""
	if key, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Explorer\Shell Folders`, registry.QUERY_VALUE); err == nil {
		dir, _, _ = key.GetStringValue("Desktop")
		key.Close()
	}
	if strings.TrimSpace(dir) == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, "Desktop")
		}
	}
	return filepath.Join(dir, shortcutName)
}

func currentExe() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	abs, _ := filepath.Abs(exe)
	return abs
}

// withShell выполняет fn с объектом WScript.Shell (COM привязан к потоку).
func withShell(fn func(shell *ole.IDispatch) error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	_ = ole.CoInitialize(0)
	defer ole.CoUninitialize()

	unknown, err := oleutil.CreateObject("WScript.Shell")
	if err != nil {
		return fmt.Errorf("WScript.Shell недоступен: %w", err)
	}
	defer unknown.Release()
	shell, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return err
	}
	defer shell.Release()
	return fn(shell)
}

func writeShortcut(lnk, target string) error {
	return withShell(func(shell *ole.IDispatch) error {
		res, err := oleutil.CallMethod(shell, "CreateShortcut", lnk)
		if err != nil {
			return err
		}
		sc := res.ToIDispatch()
		defer sc.Release()
		for prop, val := range map[string]string{
			"TargetPath":       target,
			"WorkingDirectory": filepath.Dir(target),
			"IconLocation":     target + ",0",
			"Description":      "T31K Binder — биндер фраз. Автор: T31K",
		} {
			if _, err := oleutil.PutProperty(sc, prop, val); err != nil {
				return err
			}
		}
		_, err = oleutil.CallMethod(sc, "Save")
		return err
	})
}

func shortcutTarget(lnk string) string {
	target := ""
	_ = withShell(func(shell *ole.IDispatch) error {
		res, err := oleutil.CallMethod(shell, "CreateShortcut", lnk)
		if err != nil {
			return err
		}
		sc := res.ToIDispatch()
		defer sc.Release()
		v, err := oleutil.GetProperty(sc, "TargetPath")
		if err == nil {
			target = v.ToString()
		}
		return nil
	})
	return target
}

// ensureDesktopShortcut создаёт ярлык при первом запуске и чинит его, если
// exe перенесли. Ярлык, который пользователь удалил сам, заново не создаётся.
func (a *App) ensureDesktopShortcut() {
	if testMode {
		return
	}
	exe := currentExe()
	if exe == "" {
		return
	}
	lnk := desktopShortcutPath()
	if _, err := os.Stat(lnk); err == nil {
		if t := shortcutTarget(lnk); t != "" && !strings.EqualFold(t, exe) {
			_ = writeShortcut(lnk, exe)
		}
	} else if !a.snapshot().Settings.ShortcutCreated {
		_ = writeShortcut(lnk, exe)
	}

	a.mu.Lock()
	done := a.cfg.Settings.ShortcutCreated
	a.cfg.Settings.ShortcutCreated = true
	cfg := a.cfg
	a.mu.Unlock()
	if !done {
		_ = saveConfig(cfg)
	}
}

// CreateDesktopShortcut — кнопка в настройках.
func (a *App) CreateDesktopShortcut() error {
	exe := currentExe()
	if exe == "" {
		return fmt.Errorf("не удалось определить путь к программе")
	}
	if err := writeShortcut(desktopShortcutPath(), exe); err != nil {
		return fmt.Errorf("ярлык не создан: %w", err)
	}
	return nil
}

func (a *App) HasDesktopShortcut() bool {
	_, err := os.Stat(desktopShortcutPath())
	return err == nil
}
