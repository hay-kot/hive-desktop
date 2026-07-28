package httpapi

import (
	"io"
	"net/http"

	"github.com/hay-kot/httpkit/server"

	"github.com/hay-kot/hive-desktop/internal/app"
)

// nodeImageView reports which node a feed-mark image belongs to and whether one
// is set; the bytes come from the GET endpoint.
type nodeImageView struct {
	FlowID   string `json:"flowId"`
	NodeID   string `json:"nodeId"`
	HasImage bool   `json:"hasImage"`
}

// GetNodeImage returns a source node's feed-mark image as a PNG, or 404 when it
// has none.
func (ctrl *Controller) GetNodeImage(w http.ResponseWriter, r *http.Request) error {
	flowID, nodeID := r.PathValue("flowId"), r.PathValue("nodeId")
	data, err := ctrl.core.Flows.NodeImage(r.Context(), flowID, nodeID)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return app.Errorf(app.KindNotFound, "node %q in flow %q has no image", nodeID, flowID)
	}
	w.Header().Set("Content-Type", "image/png")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
	return nil
}

// SetNodeImage stores a source node's feed-mark image from the raw request body
// (PNG/JPEG/GIF/WebP; the core sniffs the format) and records it on the node.
func (ctrl *Controller) SetNodeImage(w http.ResponseWriter, r *http.Request) error {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxImageUpload))
	if err != nil {
		return app.Errorf(app.KindInvalid, "could not read the image (is it larger than the size limit?)")
	}
	flowID, nodeID := r.PathValue("flowId"), r.PathValue("nodeId")
	if _, err := ctrl.core.Flows.SetNodeImage(r.Context(), flowID, nodeID, body); err != nil {
		return err
	}
	return server.JSON(w, http.StatusOK, nodeImageView{FlowID: flowID, NodeID: nodeID, HasImage: true})
}

// ClearNodeImage removes a source node's feed-mark image so it reverts to its icon.
func (ctrl *Controller) ClearNodeImage(w http.ResponseWriter, r *http.Request) error {
	flowID, nodeID := r.PathValue("flowId"), r.PathValue("nodeId")
	if err := ctrl.core.Flows.ClearNodeImage(r.Context(), flowID, nodeID); err != nil {
		return err
	}
	return server.JSON(w, http.StatusOK, nodeImageView{FlowID: flowID, NodeID: nodeID, HasImage: false})
}
