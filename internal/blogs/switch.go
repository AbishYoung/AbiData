package blogs

import (
	"context"
	"errors"

	"abidata/internal/admin"
)

// SettingPublicBlogs is the admin setting that turns public blogs on ("true") or off.
const SettingPublicBlogs = "public_blogs"

// InstallSwitch is the install-wide switch for public blogs (ADR 0017). It is
// off until an administrator turns it on, and ABIDATA_PUBLIC_BLOGS=false
// forbids it for good, whatever the setting says.
type InstallSwitch struct {
	Admin *admin.Store
	// Allowed is ABIDATA_PUBLIC_BLOGS (default true).
	Allowed bool
}

// On reports whether blogs are served; forced is true when the environment forbids them.
func (w InstallSwitch) On(ctx context.Context) (on, forced bool) {
	if !w.Allowed {
		return false, true
	}
	if w.Admin == nil {
		return false, false
	}
	v, err := w.Admin.GetSetting(ctx, SettingPublicBlogs)
	if errors.Is(err, admin.ErrNotFound) {
		return false, false // default off
	} else if err != nil {
		return false, false // fail closed
	}
	return v == "true", false
}

// Enabled is On without the reason, for Service.PublicEnabled.
func (w InstallSwitch) Enabled(ctx context.Context) bool {
	on, _ := w.On(ctx)
	return on
}
