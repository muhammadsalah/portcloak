package app

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// applicationMenu builds the menu bar PortCloak ships with.
//
// It exists because leaving it nil is not "no menu": Wails substitutes
// DefaultApplicationMenu(), which is a developer's menu wearing the app's name.
// Two of its entries have no business in a released build —
//
//   - View carries Reload, Force Reload and Toggle DevTools. Reloading the
//     webview mid-capture drops every subscription the Activity screen holds
//     while the job carries on in the engine, so the UI goes quiet on work that
//     is still running.
//   - Help carries "Learn More", whose handler is
//     `Window.Current().SetURL("https://wails.io")`. It navigates the main
//     window away from the app to a framework's marketing site, and because the
//     app has no back affordance, the only way home is to quit.
//
// What is kept is what macOS genuinely needs. The App menu is where the platform
// puts About and Quit. The Edit menu is not decoration: a WKWebView gets
// Cmd+C/V/X/A from the first responder chain via these very items, so dropping
// it silently breaks copy and paste in every text field in the app.
//
// On Windows and Linux this menu is built and then never attached, because the
// window sets UseApplicationMenu false and defines no window menu of its own —
// those platforms show no menu bar at all, which is what a single-window tool
// should look like there.
func applicationMenu(app *application.App) *application.Menu {
	menu := app.NewMenu()

	// AddRole skips a role whose constructor returns nil, and NewAppMenu
	// returns nil off macOS — so this same list is correct on all three
	// platforms without a build tag or a runtime.GOOS branch.
	menu.AddRole(application.AppMenu)
	menu.AddRole(application.EditMenu)
	menu.AddRole(application.WindowMenu)

	// Non-production builds get the reload and inspector entries back, under a
	// menu that is obviously not for users. In a production build this is
	// compiled out entirely rather than hidden at runtime.
	addDeveloperMenu(menu)

	return menu
}
