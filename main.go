package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

func main() {
	profiles, err := newProfileService("")
	if err != nil {
		log.Fatal(err)
	}
	app := application.New(application.Options{
		Name:        "Isolate",
		Icon:        appIcon,
		Services:    []application.Service{application.NewService(profiles), application.NewService(newAndroidService())},
		Description: "Isolate desktop application",
		Assets:      application.AssetOptions{Handler: application.AssetFileServerFS(assets)},
		Linux:       application.LinuxOptions{ApplicationID: "app.isolate.desktop", ProgramName: "isolate"},
		Mac:         application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
	})
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "isolate-main", Title: "Isolate",
		Width: 1200, Height: 800, MinWidth: 480, MinHeight: 320,
		Frameless: true, BackgroundColour: application.NewRGB(248, 249, 250), URL: "/",
		Linux: application.LinuxWindow{Icon: appIcon},
	})
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
