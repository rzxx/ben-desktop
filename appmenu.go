package main

import (
	"ben/desktop/internal/appupdate"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func installApplicationMenu(app *application.App, updates *appupdate.CheckRunner) {
	if app == nil {
		return
	}
	menu := app.Menu.New()
	app.Menu.SetApplicationMenu(menu)
	appMenu := menu.AddSubmenu("App")
	appMenu.Add("Check for Updates...").OnClick(func(*application.Context) {
		if updates != nil {
			updates.StartManualCheck(nil)
		}
	})
	appMenu.AddSeparator()
	appMenu.Add("Quit").OnClick(func(*application.Context) {
		app.Quit()
	})
}
