package main

import (
	"embed"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

const (
	appTitle   = "T31K Binder"
	appVersion = "1.0.2"
)

var testMode bool

func main() {
	// BINDER_TEST=1 — режим проверки: окно стартует скрытым, ярлык не
	// трогается, а WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS (порт отладки)
	// сохраняется. Обычному запуску отладочный порт не нужен.
	testMode = os.Getenv("BINDER_TEST") == "1"
	if !testMode {
		_ = os.Unsetenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS")
	}
	openLog()
	logf("запуск %s, версия %s, аргументы %v", currentExe(), appVersion, os.Args[1:])
	updated := prepareAfterUpdate(os.Args[1:])

	cfg := loadConfig()
	app := NewApp(cfg)
	app.updated = updated

	w, h := cfg.Settings.Width, cfg.Settings.Height
	if w < 320 {
		w = 400
	}
	if h < 260 {
		h = 560
	}

	logf("окно запускается")
	err := wails.Run(&options.App{
		Title:       appTitle,
		Width:       w,
		Height:      h,
		MinWidth:    320,
		MinHeight:   260,
		Frameless:   true,
		StartHidden: testMode,
		// Иначе Wails передаёт WebView2 свои аргументы и порт отладки не откроется.
		EnableFraudulentWebsiteDetection: testMode,
		// Прозрачность оверлея задаётся всему окну средствами Windows
		// (setWindowOpacity), сам фон страницы непрозрачный.
		BackgroundColour: &options.RGBA{R: 20, G: 16, B: 34, A: 255},
		AlwaysOnTop:      cfg.Settings.AlwaysOnTop,
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        app.onStartup,
		OnDomReady:       app.onDomReady,
		OnBeforeClose:    app.onBeforeClose,
		OnShutdown:       app.onShutdown,
		Bind:             []any{app},
		// Повторный запуск не открывает вторую копию, а показывает окно.
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               "t31k-binder-5c1f0e2a",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) { app.showWindow(true) },
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			Theme:                windows.Dark,
		},
	})
	if err != nil {
		logf("wails.Run: %v", err)
		os.Exit(1)
	}
	logf("выход")
}
