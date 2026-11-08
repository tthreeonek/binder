package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"
)

type App struct {
	ctx context.Context

	mu        sync.Mutex
	cfg       Config
	errs      map[string]string
	recording bool

	hk      *hotkeyManager
	hwnd    atomic.Uintptr
	target  atomic.Uintptr // последнее активное чужое окно — туда вставляем по клику
	pasteMu sync.Mutex
	pid     uint32
	updated bool // запущены сразу после обновления
	tray    *trayIcon
}

// State — то, что получает страница.
type State struct {
	Config  Config            `json:"config"`
	Errors  map[string]string `json:"errors"`
	Version string            `json:"version"`
	Path    string            `json:"path"`
	Updated bool              `json:"updated"`
	Repo    string            `json:"repo"`
}

func NewApp(cfg Config) *App {
	return &App{cfg: cfg, errs: map[string]string{}, pid: uint32(os.Getpid())}
}

func (a *App) onStartup(ctx context.Context) {
	a.ctx = ctx
	a.hk = newHotkeyManager(a.onHotkey)
	a.applyHotkeys()
	go a.trackForeground()
	go a.autoCheckUpdates()
	go a.ensureDesktopShortcut()

	a.tray = &trayIcon{
		onToggle: func() {
			h := a.hwnd.Load()
			if h != 0 && isWindowVisible(h) && !isIconic(h) {
				a.hideWindow()
			} else {
				a.showWindow(true)
			}
		},
		onSettings: func() {
			a.showWindow(true)
			wruntime.EventsEmit(a.ctx, "binder:open-settings")
		},
		onQuit: a.Quit,
		tip: func() string {
			t := "T31K Binder — работает"
			if hk := a.snapshot().Settings.ToggleHotkey; !hk.Empty() {
				t += "\nОткрыть / скрыть: " + hotkeyLabel(hk)
			}
			return t
		},
	}
	startTray(a.tray)
}

// hotkeyLabel — сочетание в виде «Ctrl + Alt + B».
func hotkeyLabel(h Hotkey) string {
	var parts []string
	if h.Ctrl {
		parts = append(parts, "Ctrl")
	}
	if h.Alt {
		parts = append(parts, "Alt")
	}
	if h.Shift {
		parts = append(parts, "Shift")
	}
	if h.Win {
		parts = append(parts, "Win")
	}
	key := h.Code
	switch {
	case strings.HasPrefix(key, "Digit"):
		key = strings.TrimPrefix(key, "Digit")
	case strings.HasPrefix(key, "Key"):
		key = strings.TrimPrefix(key, "Key")
	case strings.HasPrefix(key, "Numpad"):
		key = "Num " + strings.TrimPrefix(key, "Numpad")
	}
	return strings.Join(append(parts, key), " + ")
}

func (a *App) onDomReady(ctx context.Context) {
	s := a.snapshot().Settings
	if s.PositionRemembered {
		wruntime.WindowSetPosition(ctx, s.X, s.Y)
	}
	go func() {
		for i := 0; i < 50 && a.hwnd.Load() == 0; i++ {
			if h := findOwnWindow(a.pid, appTitle); h != 0 {
				a.hwnd.Store(h)
				setWindowOpacity(h, a.snapshot().Settings.Opacity)
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
}

func (a *App) onBeforeClose(ctx context.Context) bool {
	a.rememberGeometry()
	return false
}

func (a *App) onShutdown(ctx context.Context) {
	if a.hk != nil {
		a.hk.Set(nil)
	}
	a.tray.Remove()
}

func (a *App) snapshot() Config {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg
}

func (a *App) rememberGeometry() {
	h := a.hwnd.Load()
	if h == 0 || !isWindowVisible(h) || isIconic(h) {
		return
	}
	r, ok := windowRect(h)
	if !ok {
		return
	}
	a.mu.Lock()
	s := &a.cfg.Settings
	s.X, s.Y = int(r.left), int(r.top)
	s.Width, s.Height = int(r.right-r.left), int(r.bottom-r.top)
	s.PositionRemembered = true
	cfg := a.cfg
	a.mu.Unlock()
	_ = saveConfig(cfg)
}

// ---------- глобальные сочетания ----------

func (a *App) bindings() []binding {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.recording {
		return nil
	}
	b := []binding{{Key: "toggle", Hotkey: a.cfg.Settings.ToggleHotkey}}
	for _, p := range a.cfg.Phrases {
		b = append(b, binding{Key: p.ID, Hotkey: p.Hotkey})
	}
	return b
}

func (a *App) applyHotkeys() {
	errs := a.hk.Set(a.bindings())
	a.mu.Lock()
	if !a.recording {
		a.errs = errs
	}
	a.mu.Unlock()
}

func (a *App) onHotkey(key string) {
	if key == "toggle" {
		a.toggleWindow()
		return
	}
	text, ok := a.phraseText(key)
	if !ok {
		return
	}
	if err := a.paste(text, true); err != nil {
		wruntime.EventsEmit(a.ctx, "binder:error", err.Error())
		return
	}
	wruntime.EventsEmit(a.ctx, "binder:pasted", key)
}

func (a *App) phraseText(id string) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, p := range a.cfg.Phrases {
		if p.ID == id {
			return p.Text, true
		}
	}
	return "", false
}

// paste вставляет текст в активное окно: через буфер обмена и Ctrl+V либо
// посимвольным вводом — как выбрано в настройках.
func (a *App) paste(text string, fromHotkey bool) error {
	a.pasteMu.Lock()
	defer a.pasteMu.Unlock()
	s := a.snapshot().Settings

	if fromHotkey {
		if err := releaseModifiers(700 * time.Millisecond); err != nil {
			return err
		}
	}

	if s.PasteMode == "type" {
		if err := typeText(text); err != nil {
			return err
		}
	} else {
		old, hadText := "", false
		if s.RestoreClipboard {
			old, hadText = clipboardText()
		}
		if err := setClipboardText(text, true); err != nil {
			return err
		}
		time.Sleep(30 * time.Millisecond)
		if err := sendCtrlV(); err != nil {
			return err
		}
		if s.RestoreClipboard && hadText {
			go func() {
				// Приложение читает буфер не мгновенно — даём ему время.
				time.Sleep(450 * time.Millisecond)
				if cur, ok := clipboardText(); ok && cur == text {
					_ = setClipboardText(old, false)
				}
			}()
		}
	}
	if s.PressEnter {
		time.Sleep(60 * time.Millisecond)
		return sendEnter()
	}
	return nil
}

// trackForeground запоминает последнее активное окно другой программы,
// чтобы вставка по клику в биндере уходила туда, а не в сам биндер.
func (a *App) trackForeground() {
	for {
		h := foregroundWindow()
		if h != 0 && windowPID(h) != a.pid {
			a.target.Store(h)
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// ---------- окно ----------

func (a *App) toggleWindow() {
	h := a.hwnd.Load()
	if h == 0 {
		return
	}
	if isWindowVisible(h) && !isIconic(h) {
		a.hideWindow()
	} else {
		a.showWindow(!a.snapshot().Settings.ShowWithoutFocus)
	}
}

func (a *App) showWindow(activate bool) {
	h := a.hwnd.Load()
	if h == 0 {
		wruntime.WindowShow(a.ctx)
		return
	}
	if isIconic(h) {
		showWindowCmd(h, swRestore)
	}
	if activate {
		showWindowCmd(h, swShow)
		setForeground(h)
	} else {
		showWindowCmd(h, swShowNoActivate)
	}
	setTopmost(h, a.snapshot().Settings.AlwaysOnTop)
	wruntime.EventsEmit(a.ctx, "binder:shown")
}

func (a *App) hideWindow() {
	a.rememberGeometry()
	if h := a.hwnd.Load(); h != 0 {
		showWindowCmd(h, swHide)
		return
	}
	wruntime.WindowHide(a.ctx)
}

// ---------- методы для страницы ----------

func (a *App) GetState() State {
	a.mu.Lock()
	defer a.mu.Unlock()
	errs := map[string]string{}
	for k, v := range a.errs {
		errs[k] = v
	}
	return State{Config: a.cfg, Errors: errs, Version: appVersion, Path: configPath(),
		Updated: a.updated, Repo: "https://github.com/" + updateRepo}
}

// SaveConfig сохраняет фразы и настройки и перерегистрирует сочетания.
func (a *App) SaveConfig(cfg Config) (State, error) {
	cfg.normalize()
	seen := map[string]bool{}
	for i := range cfg.Phrases {
		p := &cfg.Phrases[i]
		if p.ID == "" || seen[p.ID] {
			p.ID = "p" + time.Now().Format("150405.000000") + string(rune('a'+i%26))
		}
		seen[p.ID] = true
	}
	a.mu.Lock()
	// Геометрию окна страница не знает — оставляем свою.
	g := a.cfg.Settings
	cfg.Settings.X, cfg.Settings.Y = g.X, g.Y
	cfg.Settings.Width, cfg.Settings.Height = g.Width, g.Height
	cfg.Settings.PositionRemembered = g.PositionRemembered
	cfg.Settings.ShortcutCreated = g.ShortcutCreated
	topChanged := g.AlwaysOnTop != cfg.Settings.AlwaysOnTop
	a.cfg = cfg
	a.mu.Unlock()

	if topChanged {
		wruntime.WindowSetAlwaysOnTop(a.ctx, cfg.Settings.AlwaysOnTop)
	}
	a.SetOpacity(cfg.Settings.Opacity)
	a.tray.Refresh()
	a.applyHotkeys()
	if err := saveConfig(cfg); err != nil {
		return a.GetState(), errors.New("не удалось сохранить настройки: " + err.Error())
	}
	return a.GetState(), nil
}

// SetRecording снимает глобальные сочетания на время записи нового бинда,
// иначе уже занятые сочетания не дошли бы до окна.
func (a *App) SetRecording(on bool) State {
	a.mu.Lock()
	a.recording = on
	a.mu.Unlock()
	a.applyHotkeys()
	return a.GetState()
}

// PastePhrase вставляет фразу в окно, которое было активно до клика по биндеру.
func (a *App) PastePhrase(id string) error {
	text, ok := a.phraseText(id)
	if !ok {
		return errors.New("фраза не найдена")
	}
	t := a.target.Load()
	if t == 0 || !isWindow(t) {
		return errors.New("не найдено окно для вставки — переключитесь в нужное окно и используйте сочетание")
	}
	go func() {
		if !setForeground(t) {
			wruntime.EventsEmit(a.ctx, "binder:error", "Windows не дала переключиться в окно — используйте сочетание клавиш")
			return
		}
		time.Sleep(150 * time.Millisecond)
		if err := a.paste(text, false); err != nil {
			wruntime.EventsEmit(a.ctx, "binder:error", err.Error())
			return
		}
		wruntime.EventsEmit(a.ctx, "binder:pasted", id)
	}()
	return nil
}

// SetOpacity меняет прозрачность окна сразу, пока тянут ползунок
// (сохраняется отдельно, через SaveConfig).
func (a *App) SetOpacity(percent int) {
	if h := a.hwnd.Load(); h != 0 {
		setWindowOpacity(h, percent)
	}
}

func (a *App) CopyPhrase(id string) error {
	text, ok := a.phraseText(id)
	if !ok {
		return errors.New("фраза не найдена")
	}
	return setClipboardText(text, false)
}

func (a *App) Hide()     { a.hideWindow() }
func (a *App) Minimize() { a.rememberGeometry(); wruntime.WindowMinimise(a.ctx) }
func (a *App) Quit()     { a.rememberGeometry(); wruntime.Quit(a.ctx) }

func (a *App) ResetConfig() (State, error) {
	d := defaultConfig()
	return a.SaveConfig(d)
}

// findOwnWindow ищет верхнее окно нашего процесса с заданным заголовком.
func findOwnWindow(pid uint32, title string) uintptr {
	if h := findWindowByTitle(title); h != 0 && windowPID(h) == pid {
		return h
	}
	var found uintptr
	cb := windows.NewCallback(func(h windows.HWND, _ uintptr) uintptr {
		if windowPID(uintptr(h)) != pid {
			return 1
		}
		buf := make([]uint16, 256)
		n, _, _ := pGetWindowTextW.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if strings.EqualFold(windows.UTF16ToString(buf[:n]), title) {
			found = uintptr(h)
			return 0
		}
		return 1
	})
	_ = windows.EnumWindows(cb, unsafe.Pointer(nil))
	return found
}
