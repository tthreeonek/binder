package main

import (
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Значок в трее (область уведомлений справа внизу). У Wails v2 своего трея
// нет, поэтому значок держит отдельное скрытое окно в своём потоке ОС.

var (
	shell32 = windows.NewLazySystemDLL("shell32.dll")

	pShellNotifyIconW       = shell32.NewProc("Shell_NotifyIconW")
	pExtractIconExW         = shell32.NewProc("ExtractIconExW")
	pRegisterClassExW       = user32.NewProc("RegisterClassExW")
	pCreateWindowExW        = user32.NewProc("CreateWindowExW")
	pDefWindowProcW         = user32.NewProc("DefWindowProcW")
	pDestroyWindow          = user32.NewProc("DestroyWindow")
	pTranslateMessage       = user32.NewProc("TranslateMessage")
	pDispatchMessageW       = user32.NewProc("DispatchMessageW")
	pPostMessageW           = user32.NewProc("PostMessageW")
	pRegisterWindowMessageW = user32.NewProc("RegisterWindowMessageW")
	pCreatePopupMenu        = user32.NewProc("CreatePopupMenu")
	pAppendMenuW            = user32.NewProc("AppendMenuW")
	pSetMenuDefaultItem     = user32.NewProc("SetMenuDefaultItem")
	pTrackPopupMenu         = user32.NewProc("TrackPopupMenu")
	pDestroyMenu            = user32.NewProc("DestroyMenu")
	pGetCursorPos           = user32.NewProc("GetCursorPos")
	pGetModuleHandleW       = kernel32.NewProc("GetModuleHandleW")
	pLoadIconW              = user32.NewProc("LoadIconW")
	pPostQuitMessage        = user32.NewProc("PostQuitMessage")
)

const (
	nimAdd        = 0
	nimModify     = 1
	nimDelete     = 2
	nimSetVersion = 4
	nifMessage    = 0x1
	nifIcon       = 0x2
	nifTip        = 0x4
	nifShowTip    = 0x80

	wmTray        = wmApp + 2
	wmTrayQuit    = wmApp + 3
	wmClose       = 0x0010
	wmLButtonUp   = 0x0202
	wmRButtonUp   = 0x0205
	wmContextMenu = 0x007B
	wmNull        = 0x0000

	mfString    = 0x0
	mfSeparator = 0x800

	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100
	tpmNoNotify    = 0x0080

	menuToggle   = 1
	menuSettings = 2
	menuQuit     = 3
)

type notifyIconData struct {
	cbSize           uint32
	hWnd             uintptr
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            uintptr
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         windows.GUID
	hBalloonIcon     uintptr
}

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type point struct{ x, y int32 }

type trayIcon struct {
	hwnd           uintptr
	nid            notifyIconData
	taskbarCreated uintptr
	onToggle       func()
	onSettings     func()
	onQuit         func()
	tip            func() string
}

// startTray поднимает значок в трее; возвращается, когда значок добавлен.
func startTray(t *trayIcon) {
	ready := make(chan struct{})
	go t.loop(ready)
	<-ready
}

func (t *trayIcon) loop(ready chan struct{}) {
	runtime.LockOSThread()

	hInst, _, _ := pGetModuleHandleW.Call(0)
	className, _ := windows.UTF16PtrFromString("T31KBinderTray")
	wc := wndClassEx{
		lpfnWndProc:   windows.NewCallback(t.wndProc),
		hInstance:     hInst,
		lpszClassName: className,
	}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	title, _ := windows.UTF16PtrFromString("T31K Binder tray")
	t.hwnd, _, _ = pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)),
		0, 0, 0, 0, 0, 0, 0, hInst, 0)

	tbc, _ := windows.UTF16PtrFromString("TaskbarCreated")
	t.taskbarCreated, _, _ = pRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(tbc)))

	t.nid.cbSize = uint32(unsafe.Sizeof(t.nid))
	t.nid.hWnd = t.hwnd
	t.nid.uID = 1
	t.nid.uFlags = nifMessage | nifIcon | nifTip | nifShowTip
	t.nid.uCallbackMessage = wmTray
	t.nid.hIcon = appSmallIcon()
	t.add()
	close(ready)

	var m msg
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func (t *trayIcon) setTip() {
	tip := "T31K Binder"
	if t.tip != nil {
		tip = t.tip()
	}
	u := windows.StringToUTF16(tip)
	if len(u) > len(t.nid.szTip) {
		u = append(u[:len(t.nid.szTip)-1], 0)
	}
	t.nid.szTip = [128]uint16{}
	copy(t.nid.szTip[:], u)
}

func (t *trayIcon) add() {
	t.setTip()
	pShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&t.nid)))
	t.nid.uVersion = 4 // NOTIFYICON_VERSION_4
	pShellNotifyIconW.Call(nimSetVersion, uintptr(unsafe.Pointer(&t.nid)))
}

// Refresh обновляет подсказку (например, после смены сочетания).
func (t *trayIcon) Refresh() {
	if t == nil || t.hwnd == 0 {
		return
	}
	t.setTip()
	pShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&t.nid)))
}

// Remove убирает значок из трея (при выходе).
func (t *trayIcon) Remove() {
	if t == nil || t.hwnd == 0 {
		return
	}
	pShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&t.nid)))
	pPostMessageW.Call(t.hwnd, wmTrayQuit, 0, 0)
}

func (t *trayIcon) wndProc(hwnd, message, wParam, lParam uintptr) uintptr {
	switch {
	case message == wmTray:
		// NOTIFYICON_VERSION_4: событие мыши — в младшем слове lParam.
		switch lParam & 0xFFFF {
		case wmLButtonUp:
			go t.onToggle()
		case wmRButtonUp, wmContextMenu:
			t.showMenu()
		}
		return 0
	case message == wmTrayQuit:
		pDestroyWindow.Call(hwnd)
		pPostQuitMessage.Call(0)
		return 0
	case t.taskbarCreated != 0 && message == t.taskbarCreated:
		// Проводник перезапустился — значок нужно добавить заново.
		t.add()
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, message, wParam, lParam)
	return r
}

func (t *trayIcon) showMenu() {
	menu, _, _ := pCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer pDestroyMenu.Call(menu)
	item := func(id uintptr, text string) {
		p, _ := windows.UTF16PtrFromString(text)
		pAppendMenuW.Call(menu, mfString, id, uintptr(unsafe.Pointer(p)))
	}
	item(menuToggle, "Показать / скрыть")
	item(menuSettings, "Настройки")
	pAppendMenuW.Call(menu, mfSeparator, 0, 0)
	item(menuQuit, "Выход")
	pSetMenuDefaultItem.Call(menu, menuToggle, 0)

	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	// Без этого меню не закрывается по клику мимо него.
	setForeground(t.hwnd)
	cmd, _, _ := pTrackPopupMenu.Call(menu, tpmRightButton|tpmReturnCmd|tpmNoNotify,
		uintptr(pt.x), uintptr(pt.y), 0, t.hwnd, 0)
	pPostMessageW.Call(t.hwnd, wmNull, 0, 0)

	switch cmd {
	case menuToggle:
		go t.onToggle()
	case menuSettings:
		go t.onSettings()
	case menuQuit:
		go t.onQuit()
	}
}

// appSmallIcon — маленькая иконка из ресурсов самого exe.
func appSmallIcon() uintptr {
	if exe := currentExe(); exe != "" {
		p, _ := windows.UTF16PtrFromString(exe)
		var small uintptr
		if n, _, _ := pExtractIconExW.Call(uintptr(unsafe.Pointer(p)), 0, 0, uintptr(unsafe.Pointer(&small)), 1); n > 0 && small != 0 {
			return small
		}
	}
	h, _, _ := pLoadIconW.Call(0, 32512) // IDI_APPLICATION
	return h
}
