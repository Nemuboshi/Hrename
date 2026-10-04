package main

import (
	"embed"
	"log"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed frontend/src
var assets embed.FS

// The WebView2 runtime insists on a writable user data folder: it keeps
// GPU and shader caches, the component cache, and crash dumps there. The
// app itself stores nothing that needs to survive a run, so the folder
// lives in the temp directory and is simply left there — the OS temp
// cleaner collects it.
func webviewDataPath() string {
	return filepath.Join(os.TempDir(), "RenameLite.WebView2")
}

func main() {
	app := NewApp()
	err := wails.Run(&options.App{
		Title: "RenameLite",
		// A starting size for the brief moment before the front end
		// measures the content and refits the window to it. The real size
		// comes from the page, so it adapts to each platform's font
		// metrics and frame.
		Width:            906,
		Height:           526,
		DisableResize:    true,
		StartHidden:      true,
		BackgroundColour: &options.RGBA{R: 240, G: 240, B: 240, A: 255},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup: app.startup,
		Windows: &windows.Options{
			// Without this the WebView2 runtime derives the folder name
			// from the executable and writes %AppData%\renamelite.exe.
			// It creates its "EBWebView" folder inside the path we give.
			WebviewUserDataPath: webviewDataPath(),
		},
		Bind: []interface{}{
			app,
		},
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop: true,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
