// Package perms is the catalog of granular permissions a custom role can grant.
//
// Each permission is "<area>.<capability>" (e.g. fuel.price_calc, vehicles.edit).
// Feature ties a permission to an account-level entitlement (features.Registry):
// a role can only usefully grant a permission whose Feature the account has
// enabled. Feature "" means always available (not gated at the account level).
package perms

// Perm is one grantable capability.
type Perm struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Group   string `json:"group"`   // UI grouping
	Feature string `json:"feature"` // account feature that must be enabled ("" = always)
}

// Catalog is the full, ordered permission matrix (view + manage/edit per area,
// plus named sub-capabilities). Grouped to mirror the app.
var Catalog = []Perm{
	{Key: "dashboard.view", Label: "View dashboard", Group: "Dashboard", Feature: "dashboard"},
	{Key: "dashboard.ai", Label: "AI dashboard", Group: "Dashboard", Feature: "ai_dashboard"},

	{Key: "live.view", Label: "View live tracking", Group: "Live Tracking", Feature: "live"},
	{Key: "live.history", Label: "View trip history", Group: "Live Tracking", Feature: "live"},

	{Key: "fuel.view", Label: "View fuel & cost", Group: "Fuel", Feature: "live"},
	{Key: "fuel.price_calc", Label: "Fuel price calculation", Group: "Fuel", Feature: "live"},

	{Key: "geofence.view", Label: "View geofences", Group: "Geofence", Feature: "geofence"},
	{Key: "geofence.manage", Label: "Manage geofences", Group: "Geofence", Feature: "geofence"},

	{Key: "controltower.view", Label: "View control tower", Group: "Control Tower", Feature: "controltower"},

	{Key: "notifications.view", Label: "View notifications", Group: "Notifications", Feature: "notifications"},
	{Key: "notifications.manage", Label: "Manage notification rules", Group: "Notifications", Feature: "notifications"},

	{Key: "reports.view", Label: "View reports", Group: "Reporting", Feature: "reports"},
	{Key: "reports.custom", Label: "Custom report builder", Group: "Reporting", Feature: "custom_report"},
	{Key: "reports.ai", Label: "AI reporting", Group: "Reporting", Feature: "ai_reporting"},

	{Key: "camera.view", Label: "View cameras (TrackVision)", Group: "Camera", Feature: "camera"},

	{Key: "share_link.view", Label: "View share links", Group: "Share Link", Feature: "share_link"},
	{Key: "share_link.create", Label: "Generate share link", Group: "Share Link", Feature: "generate_link"},

	{Key: "maintenance.view", Label: "View maintenance", Group: "Fleet Management", Feature: "maintenance"},
	{Key: "maintenance.manage", Label: "Manage maintenance", Group: "Fleet Management", Feature: "maintenance"},
	{Key: "hangars.view", Label: "View workshops", Group: "Fleet Management", Feature: "hangars"},
	{Key: "hangars.manage", Label: "Manage workshops", Group: "Fleet Management", Feature: "hangars"},
	{Key: "license.view", Label: "View licenses", Group: "Fleet Management", Feature: "license"},
	{Key: "license.manage", Label: "Manage licenses", Group: "Fleet Management", Feature: "license"},

	{Key: "vehicles.view", Label: "View vehicles", Group: "Master Data", Feature: ""},
	{Key: "vehicles.edit", Label: "Add / edit vehicles", Group: "Master Data", Feature: ""},
	{Key: "drivers.view", Label: "View drivers", Group: "Master Data", Feature: ""},
	{Key: "drivers.edit", Label: "Add / edit drivers", Group: "Master Data", Feature: ""},
	{Key: "rfid.manage", Label: "Manage RFID", Group: "Master Data", Feature: "rfid"},
	{Key: "dashcams.manage", Label: "Manage dashcams", Group: "Master Data", Feature: "camera"},

	{Key: "users.manage", Label: "Manage users", Group: "Administration", Feature: ""},
	{Key: "roles.manage", Label: "Manage roles", Group: "Administration", Feature: ""},
}

// Valid reports whether key is a known permission.
func Valid(key string) bool {
	for _, p := range Catalog {
		if p.Key == key {
			return true
		}
	}
	return false
}

// Filter keeps only valid permission keys from an arbitrary input list.
func Filter(keys []string) []string {
	out := make([]string, 0, len(keys))
	seen := map[string]bool{}
	for _, k := range keys {
		if Valid(k) && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}
