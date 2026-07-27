package httpapi

import (
	"io"
	"net/http"

	"github.com/hay-kot/httpkit/server"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/profileimg"
)

// maxImageUpload caps a profile-image request body so an oversized upload is
// not buffered whole. The core enforces its own limit too; this is one byte
// over it, so a just-too-large body still reaches the core and gets its
// friendlier "too large" message rather than a truncated read.
const maxImageUpload = profileimg.MaxInputBytes + 1

// profileView is a profile (flow) as the agent API reports it: identity plus
// load status and whether an avatar is set. It is deliberately smaller than the
// Wails FlowSummary — no inline image bytes — since an agent reads the image
// from its own endpoint.
type profileView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	Valid    bool   `json:"valid"`
	HasImage bool   `json:"hasImage"`
}

type profilesResponse struct {
	Profiles []profileView `json:"profiles"`
}

func profileViewOf(id, name string, enabled, valid, hasImage bool) profileView {
	return profileView{ID: id, Name: name, Enabled: enabled, Valid: valid, HasImage: hasImage}
}

// Profiles lists every profile so an agent can discover the ids to target.
func (ctrl *Controller) Profiles(w http.ResponseWriter, r *http.Request) error {
	statuses := ctrl.core.Flows.Statuses(r.Context())
	out := make([]profileView, 0, len(statuses))
	for _, st := range statuses {
		out = append(out, profileViewOf(st.ID, st.Flow.Name, st.Flow.Enabled, st.Valid, st.Flow.Image != ""))
	}
	return server.JSON(w, http.StatusOK, profilesResponse{Profiles: out})
}

// SetProfileImage stores a profile's avatar from the raw request body. The body
// is the image bytes in any decodable format (PNG/JPEG/GIF/WebP) — the core
// sniffs and normalizes, so no Content-Type is required. Binary uploads read
// the body directly rather than through web/extractors, which decodes JSON.
func (ctrl *Controller) SetProfileImage(w http.ResponseWriter, r *http.Request) error {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxImageUpload))
	if err != nil {
		return app.Errorf(app.KindInvalid, "could not read the image (is it larger than the size limit?)")
	}
	f, err := ctrl.core.Flows.SetProfileImage(r.Context(), r.PathValue("id"), body)
	if err != nil {
		return err
	}
	return server.JSON(w, http.StatusOK, profileViewOf(f.ID, f.Name, f.Enabled, true, f.Image != ""))
}

// ClearProfileImage removes a profile's avatar so the rail reverts to the
// letter chip.
func (ctrl *Controller) ClearProfileImage(w http.ResponseWriter, r *http.Request) error {
	f, err := ctrl.core.Flows.ClearProfileImage(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return server.JSON(w, http.StatusOK, profileViewOf(f.ID, f.Name, f.Enabled, true, f.Image != ""))
}

// GetProfileImage returns a profile's stored avatar PNG, or 404 when it has
// none — so a hash referenced with no file on disk reads as absent, not an
// error.
func (ctrl *Controller) GetProfileImage(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	data, err := ctrl.core.Flows.ProfileImage(r.Context(), id)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return app.Errorf(app.KindNotFound, "profile %q has no image", id)
	}
	w.Header().Set("Content-Type", "image/png")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
	return nil
}
