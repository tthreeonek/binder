package main

import (
	"runtime"
	"strings"
	"unsafe"
)

// binding — одно глобальное сочетание и что по нему делать.
type binding struct {
	Key    string // ключ действия: "toggle" или id фразы
	Hotkey Hotkey
}

// hotkeyManager держит RegisterHotKey в отдельном потоке ОС: сочетания
// принадлежат потоку, который их зарегистрировал, туда же приходят WM_HOTKEY.
type hotkeyManager struct {
	tid    uint32
	reqs   chan hotkeyReq
	onFire func(key string)
	ready  chan struct{}
	active map[int]string
}

type hotkeyReq struct {
	binds []binding
	reply chan map[string]string // ключ действия -> текст ошибки
}

const wmApplyHotkeys = wmApp + 1

func newHotkeyManager(onFire func(key string)) *hotkeyManager {
	m := &hotkeyManager{
		reqs:   make(chan hotkeyReq, 4),
		onFire: onFire,
		ready:  make(chan struct{}),
		active: map[int]string{},
	}
	go m.loop()
	<-m.ready
	return m
}

func (m *hotkeyManager) loop() {
	runtime.LockOSThread()
	m.tid = currentThreadID()
	// Создаём очередь сообщений потока до того, как кто-то пошлёт в неё запрос.
	var mm msg
	pPeekMessageW.Call(uintptr(unsafe.Pointer(&mm)), 0, 0, 0, 0)
	close(m.ready)

	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&mm)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		switch mm.message {
		case wmHotkey:
			if key, ok := m.active[int(mm.wParam)]; ok {
				go m.onFire(key)
			}
		case wmApplyHotkeys:
			for {
				select {
				case req := <-m.reqs:
					req.reply <- m.apply(req.binds)
					continue
				default:
				}
				break
			}
		}
	}
}

func (m *hotkeyManager) apply(binds []binding) map[string]string {
	for id := range m.active {
		pUnregisterHotKey.Call(0, uintptr(id))
	}
	m.active = map[int]string{}
	errs := map[string]string{}
	id := 1
	for _, b := range binds {
		if b.Hotkey.Empty() {
			continue
		}
		vk, ok := vkFromCode(b.Hotkey.Code)
		if !ok {
			errs[b.Key] = "неизвестная клавиша " + b.Hotkey.Code
			continue
		}
		mods := uintptr(modNoRepeat)
		if b.Hotkey.Ctrl {
			mods |= modControl
		}
		if b.Hotkey.Alt {
			mods |= modAlt
		}
		if b.Hotkey.Shift {
			mods |= modShift
		}
		if b.Hotkey.Win {
			mods |= modWin
		}
		r, _, _ := pRegisterHotKey.Call(0, uintptr(id), mods, uintptr(vk))
		if r == 0 {
			errs[b.Key] = "сочетание уже занято другой программой"
			continue
		}
		m.active[id] = b.Key
		id++
	}
	return errs
}

// Set заменяет все зарегистрированные сочетания; nil — снять все.
func (m *hotkeyManager) Set(binds []binding) map[string]string {
	req := hotkeyReq{binds: binds, reply: make(chan map[string]string, 1)}
	m.reqs <- req
	pPostThreadMessageW.Call(uintptr(m.tid), wmApplyHotkeys, 0, 0)
	return <-req.reply
}

// vkFromCode переводит KeyboardEvent.code в виртуальный код клавиши Windows.
func vkFromCode(code string) (uint16, bool) {
	switch {
	case strings.HasPrefix(code, "Digit") && len(code) == 6:
		return uint16(code[5]), true
	case strings.HasPrefix(code, "Key") && len(code) == 4:
		return uint16(code[3]), true
	case strings.HasPrefix(code, "Numpad") && len(code) == 7 && code[6] >= '0' && code[6] <= '9':
		return 0x60 + uint16(code[6]-'0'), true
	case strings.HasPrefix(code, "F") && len(code) <= 3:
		n := 0
		for _, c := range code[1:] {
			if c < '0' || c > '9' {
				return 0, false
			}
			n = n*10 + int(c-'0')
		}
		if n >= 1 && n <= 24 {
			return 0x6F + uint16(n), true
		}
		return 0, false
	}
	vk, ok := map[string]uint16{
		"NumpadMultiply": 0x6A, "NumpadAdd": 0x6B, "NumpadSubtract": 0x6D,
		"NumpadDecimal": 0x6E, "NumpadDivide": 0x6F,
		"Backquote": 0xC0, "Minus": 0xBD, "Equal": 0xBB,
		"BracketLeft": 0xDB, "BracketRight": 0xDD, "Backslash": 0xDC,
		"Semicolon": 0xBA, "Quote": 0xDE, "Comma": 0xBC, "Period": 0xBE, "Slash": 0xBF,
		"Insert": 0x2D, "Delete": 0x2E, "Home": 0x24, "End": 0x23,
		"PageUp": 0x21, "PageDown": 0x22,
		"ArrowUp": 0x26, "ArrowDown": 0x28, "ArrowLeft": 0x25, "ArrowRight": 0x27,
		"Space": 0x20, "Pause": 0x13, "ScrollLock": 0x91,
	}[code]
	return vk, ok
}
