package main

import (
	"errors"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	pRegisterHotKey           = user32.NewProc("RegisterHotKey")
	pUnregisterHotKey         = user32.NewProc("UnregisterHotKey")
	pGetMessageW              = user32.NewProc("GetMessageW")
	pPostThreadMessageW       = user32.NewProc("PostThreadMessageW")
	pPeekMessageW             = user32.NewProc("PeekMessageW")
	pSendInput                = user32.NewProc("SendInput")
	pMapVirtualKeyW           = user32.NewProc("MapVirtualKeyW")
	pGetAsyncKeyState         = user32.NewProc("GetAsyncKeyState")
	pOpenClipboard            = user32.NewProc("OpenClipboard")
	pCloseClipboard           = user32.NewProc("CloseClipboard")
	pEmptyClipboard           = user32.NewProc("EmptyClipboard")
	pGetClipboardData         = user32.NewProc("GetClipboardData")
	pSetClipboardData         = user32.NewProc("SetClipboardData")
	pIsClipboardFormatAvail   = user32.NewProc("IsClipboardFormatAvailable")
	pRegisterClipboardFormatW = user32.NewProc("RegisterClipboardFormatW")
	pGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	pSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	pGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	pIsWindow                 = user32.NewProc("IsWindow")
	pIsWindowVisible          = user32.NewProc("IsWindowVisible")
	pShowWindow               = user32.NewProc("ShowWindow")
	pSetWindowPos             = user32.NewProc("SetWindowPos")
	pFindWindowW              = user32.NewProc("FindWindowW")
	pGetWindowRect            = user32.NewProc("GetWindowRect")
	pIsIconic                 = user32.NewProc("IsIconic")
	pGetWindowTextW           = user32.NewProc("GetWindowTextW")
	pGetWindowLongPtrW        = user32.NewProc("GetWindowLongPtrW")
	pSetWindowLongPtrW        = user32.NewProc("SetWindowLongPtrW")
	pSetLayeredWindowAttrs    = user32.NewProc("SetLayeredWindowAttributes")
	pGlobalAlloc              = kernel32.NewProc("GlobalAlloc")
	pGlobalLock               = kernel32.NewProc("GlobalLock")
	pGlobalUnlock             = kernel32.NewProc("GlobalUnlock")
	pGlobalSize               = kernel32.NewProc("GlobalSize")
	pGlobalFree               = kernel32.NewProc("GlobalFree")
	pGetCurrentThreadId       = kernel32.NewProc("GetCurrentThreadId")
)

const (
	modAlt      = 0x0001
	modControl  = 0x0002
	modShift    = 0x0004
	modWin      = 0x0008
	modNoRepeat = 0x4000

	wmHotkey = 0x0312
	wmApp    = 0x8000

	inputKeyboard     = 1
	keyeventfKeyUp    = 0x0002
	keyeventfUnicode  = 0x0004
	keyeventfExtended = 0x0001

	vkReturn  = 0x0D
	vkShift   = 0x10
	vkControl = 0x11
	vkMenu    = 0x12
	vkLWin    = 0x5B
	vkRWin    = 0x5C
	vkV       = 0x56
	vkMask    = 0xE8 // неназначенная клавиша: гасит меню окна при отпускании Alt

	cfUnicodeText = 13
	gmemMoveable  = 0x0002

	swHide           = 0
	swShowNoActivate = 4
	swShow           = 5
	swRestore        = 9

	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpNoActivate = 0x0010
	swpShowWindow = 0x0040
)

var (
	hwndTopmost   = ^uintptr(0)     // -1
	hwndNoTopmost = ^uintptr(0) - 1 // -2
)

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	ptX     int32
	ptY     int32
	private uint32
}

// keyboardInput — INPUT с KEYBDINPUT для 64-битной Windows (40 байт).
type keyboardInput struct {
	typ   uint32
	_     uint32
	vk    uint16
	scan  uint16
	flags uint32
	time  uint32
	extra uintptr
	_     [8]byte
}

type rect struct{ left, top, right, bottom int32 }

func currentThreadID() uint32 {
	r, _, _ := pGetCurrentThreadId.Call()
	return uint32(r)
}

// ---------- клавиатура ----------

func keyDown(vk uint16) bool {
	r, _, _ := pGetAsyncKeyState.Call(uintptr(vk))
	return r&0x8000 != 0
}

func scanOf(vk uint16) uint16 {
	r, _, _ := pMapVirtualKeyW.Call(uintptr(vk), 0)
	return uint16(r)
}

func isExtendedVK(vk uint16) bool {
	switch vk {
	case 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x2D, 0x2E, vkRWin, vkLWin:
		return true
	}
	return false
}

func vkEvent(vk uint16, up bool) keyboardInput {
	in := keyboardInput{typ: inputKeyboard, vk: vk, scan: scanOf(vk)}
	if up {
		in.flags |= keyeventfKeyUp
	}
	if isExtendedVK(vk) {
		in.flags |= keyeventfExtended
	}
	return in
}

func sendInputs(ins []keyboardInput) error {
	if len(ins) == 0 {
		return nil
	}
	n, _, err := pSendInput.Call(uintptr(len(ins)), uintptr(unsafe.Pointer(&ins[0])), unsafe.Sizeof(ins[0]))
	if int(n) != len(ins) {
		return errors.New("SendInput заблокирован: " + err.Error())
	}
	return nil
}

// releaseModifiers ждёт, пока пользователь отпустит Ctrl/Alt/Shift/Win после
// сочетания; если держит слишком долго — отпускает их программно, иначе
// вместо Ctrl+V получилось бы Ctrl+Alt+V.
func releaseModifiers(timeout time.Duration) error {
	mods := []uint16{vkMenu, vkControl, vkShift, vkLWin, vkRWin}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		held := false
		for _, m := range mods {
			if keyDown(m) {
				held = true
				break
			}
		}
		if !held {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	var ins []keyboardInput
	ins = append(ins, vkEvent(vkMask, false), vkEvent(vkMask, true))
	for _, m := range mods {
		if keyDown(m) {
			ins = append(ins, vkEvent(m, true))
		}
	}
	return sendInputs(ins)
}

func sendCtrlV() error {
	return sendInputs([]keyboardInput{
		vkEvent(vkControl, false),
		vkEvent(vkV, false),
		vkEvent(vkV, true),
		vkEvent(vkControl, true),
	})
}

func sendEnter() error {
	return sendInputs([]keyboardInput{vkEvent(vkReturn, false), vkEvent(vkReturn, true)})
}

// typeText печатает текст посимвольно (Unicode-события), без буфера обмена.
func typeText(text string) error {
	var ins []keyboardInput
	for _, u := range windows.StringToUTF16(text) {
		if u == 0 || u == '\r' {
			continue
		}
		if u == '\n' {
			ins = append(ins, vkEvent(vkReturn, false), vkEvent(vkReturn, true))
			continue
		}
		ins = append(ins,
			keyboardInput{typ: inputKeyboard, scan: u, flags: keyeventfUnicode},
			keyboardInput{typ: inputKeyboard, scan: u, flags: keyeventfUnicode | keyeventfKeyUp},
		)
	}
	// Большими пачками SendInput иногда теряет символы в играх.
	for len(ins) > 0 {
		n := min(len(ins), 40)
		if err := sendInputs(ins[:n]); err != nil {
			return err
		}
		ins = ins[n:]
		time.Sleep(5 * time.Millisecond)
	}
	return nil
}

// ---------- буфер обмена ----------

func openClipboard() error {
	for i := 0; i < 25; i++ {
		if r, _, _ := pOpenClipboard.Call(0); r != 0 {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return errors.New("буфер обмена занят другой программой")
}

// clipboardText возвращает текст из буфера; ok=false — там не текст или пусто.
func clipboardText() (string, bool) {
	if r, _, _ := pIsClipboardFormatAvail.Call(cfUnicodeText); r == 0 {
		return "", false
	}
	if openClipboard() != nil {
		return "", false
	}
	defer pCloseClipboard.Call()
	h, _, _ := pGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", false
	}
	p, _, _ := pGlobalLock.Call(h)
	if p == 0 {
		return "", false
	}
	defer pGlobalUnlock.Call(h)
	size, _, _ := pGlobalSize.Call(h)
	n := int(size / 2)
	buf := unsafe.Slice((*uint16)(unsafe.Pointer(p)), n)
	for i, c := range buf {
		if c == 0 {
			buf = buf[:i]
			break
		}
	}
	return syscall.UTF16ToString(buf), true
}

var excludeHistoryFormat = func() uintptr {
	name, _ := windows.UTF16PtrFromString("ExcludeClipboardContentFromMonitorProcessing")
	r, _, _ := pRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(name)))
	return r
}()

func globalFromUTF16(u []uint16) (uintptr, error) {
	size := uintptr(len(u) * 2)
	h, _, err := pGlobalAlloc.Call(gmemMoveable, size)
	if h == 0 {
		return 0, err
	}
	p, _, err := pGlobalLock.Call(h)
	if p == 0 {
		pGlobalFree.Call(h)
		return 0, err
	}
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(p)), len(u)), u)
	pGlobalUnlock.Call(h)
	return h, nil
}

// setClipboardText кладёт текст в буфер. hidden=true — не попадать в журнал
// буфера обмена Windows (Win+V), чтобы фразы его не засоряли.
func setClipboardText(text string, hidden bool) error {
	if err := openClipboard(); err != nil {
		return err
	}
	defer pCloseClipboard.Call()
	pEmptyClipboard.Call()
	h, err := globalFromUTF16(windows.StringToUTF16(text))
	if err != nil {
		return err
	}
	if r, _, err := pSetClipboardData.Call(cfUnicodeText, h); r == 0 {
		pGlobalFree.Call(h)
		return err
	}
	if hidden && excludeHistoryFormat != 0 {
		if hx, err := globalFromUTF16([]uint16{0}); err == nil {
			if r, _, _ := pSetClipboardData.Call(excludeHistoryFormat, hx); r == 0 {
				pGlobalFree.Call(hx)
			}
		}
	}
	return nil
}

// ---------- окна ----------

func foregroundWindow() uintptr {
	r, _, _ := pGetForegroundWindow.Call()
	return r
}

func windowPID(hwnd uintptr) uint32 {
	var pid uint32
	pGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
}

func isWindow(hwnd uintptr) bool {
	r, _, _ := pIsWindow.Call(hwnd)
	return r != 0
}

func isWindowVisible(hwnd uintptr) bool {
	r, _, _ := pIsWindowVisible.Call(hwnd)
	return r != 0
}

func isIconic(hwnd uintptr) bool {
	r, _, _ := pIsIconic.Call(hwnd)
	return r != 0
}

func setForeground(hwnd uintptr) bool {
	r, _, _ := pSetForegroundWindow.Call(hwnd)
	return r != 0
}

func showWindowCmd(hwnd uintptr, cmd int) {
	pShowWindow.Call(hwnd, uintptr(cmd))
}

func setTopmost(hwnd uintptr, on bool) {
	after := hwndNoTopmost
	if on {
		after = hwndTopmost
	}
	pSetWindowPos.Call(hwnd, after, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
}

func findWindowByTitle(title string) uintptr {
	t, _ := windows.UTF16PtrFromString(title)
	r, _, _ := pFindWindowW.Call(0, uintptr(unsafe.Pointer(t)))
	return r
}

// setWindowOpacity делает прозрачным всё окно целиком (вместе с текстом),
// как оверлей: WS_EX_LAYERED + альфа. 100 % — обычное окно без слоя.
func setWindowOpacity(hwnd uintptr, percent int) {
	const gwlExStyle = ^uintptr(19) // -20
	const wsExLayered = 0x00080000
	const lwaAlpha = 0x2
	ex, _, _ := pGetWindowLongPtrW.Call(hwnd, gwlExStyle)
	if percent >= 100 {
		if ex&wsExLayered != 0 {
			pSetWindowLongPtrW.Call(hwnd, gwlExStyle, ex&^wsExLayered)
		}
		return
	}
	if ex&wsExLayered == 0 {
		pSetWindowLongPtrW.Call(hwnd, gwlExStyle, ex|wsExLayered)
	}
	alpha := uintptr(clamp(percent, 20, 100) * 255 / 100)
	pSetLayeredWindowAttrs.Call(hwnd, 0, alpha, lwaAlpha)
}

func windowRect(hwnd uintptr) (rect, bool) {
	var r rect
	ok, _, _ := pGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return r, ok != 0
}
