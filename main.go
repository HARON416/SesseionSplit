package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	profiles, err := newProfileService("")
	if err != nil {
		log.Fatal(err)
	}
	app := application.New(application.Options{
		Name:        "SessionSplit",
		Services:    []application.Service{application.NewService(profiles)},
		Description: "SessionSplit desktop application",
		Assets:      application.AssetOptions{Handler: application.AssetFileServerFS(assets)},
		Linux:       application.LinuxOptions{ApplicationID: "app.sessionsplit.desktop", ProgramName: "sessionsplit"},
		Mac:         application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
	})
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "sessionsplit-main", Title: "SessionSplit",
		Width: 1200, Height: 800, MinWidth: 480, MinHeight: 320,
		Frameless: true, BackgroundColour: application.NewRGB(248, 249, 250), URL: "/",
	})
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
