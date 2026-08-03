package app

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/hay-kot/hive-desktop/internal/app/credentials"
	"github.com/hay-kot/hive-desktop/internal/app/flow"
	"github.com/hay-kot/hive-desktop/internal/app/profileimg"
	"github.com/hay-kot/hive-desktop/internal/app/runtime"
	"github.com/hay-kot/hive-desktop/internal/app/runtime/js"
	"github.com/hay-kot/hive-desktop/internal/app/sourcemark"
	"github.com/hay-kot/hive-desktop/internal/app/sources/webhook"
	"github.com/hay-kot/hive-desktop/internal/app/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedRef is the account a seeded starter graph fetches as; seededCreds is a
// credential store holding it, which is what makes FlowsService.Create seed a
// workspace rather than leave it empty.
const seedRef = "github/octocat"

func seededCreds(t *testing.T) credentials.Store {
	t.Helper()
	creds := credentials.NewMemoryStore()
	ref, err := credentials.ParseRef(seedRef)
	require.NoError(t, err)
	require.NoError(t, creds.Set(ref, "token"))
	return creds
}

func testImages(t *testing.T) *profileimg.Store {
	t.Helper()
	return profileimg.NewStore(t.TempDir())
}

func testMarks(t *testing.T) *sourcemark.Store {
	t.Helper()
	return sourcemark.NewStore(t.TempDir())
}

// testScripts is the language registry the service resolves function nodes
// through, wired the same way App wires the real one.
func testScripts() *runtime.ScriptRegistry {
	scripts := runtime.NewScriptRegistry()
	scripts.Register(js.New(runtime.NewScriptPool(0)))
	return scripts
}

// pngBytes encodes a small non-square opaque image the normalizer can decode.
func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 40, 24))
	for y := range 24 {
		for x := range 40 {
			img.Set(x, y, color.RGBA{R: uint8(x * 6), G: uint8(y * 10), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

// webhookServiceFlow is a minimal flow with one webhook source wired to a feed,
// so the node-image methods have a real sources.webhook node to target.
func webhookServiceFlow() flow.Flow {
	return flow.Flow{
		ID: "hooks", Name: "Hooks", Enabled: true,
		Nodes: []flow.Node{
			{ID: "hook", Type: "sources.webhook", Config: flow.NewSourceConfig(webhook.Descriptor.Type, &webhook.Config{Path: "ci"})},
			{ID: "inbox", Type: "feed", Name: "Inbox", Config: &flow.FeedConfig{}},
		},
		Wires: []flow.Wire{{From: "hook", To: "inbox"}},
	}
}

func TestFlowsServiceNodeImageLifecycle(t *testing.T) {
	flows := flow.NewFlowStore(t.TempDir(), nil)
	require.NoError(t, flows.Save(webhookServiceFlow()))
	updates := 0
	service := newFlowsService(flows, nil, seededCreds(t), testImages(t), testMarks(t), testScripts(), func() { updates++ })

	hash, err := service.SetNodeImage(t.Context(), "hooks", "hook", pngBytes(t))
	require.NoError(t, err)
	assert.True(t, sourcemark.ValidHash(hash))
	assert.Equal(t, 1, updates, "setting a node image notifies")

	data, err := service.NodeImage(t.Context(), "hooks", "hook")
	require.NoError(t, err)
	assert.NotEmpty(t, data)

	require.NoError(t, service.ClearNodeImage(t.Context(), "hooks", "hook"))
	data, err = service.NodeImage(t.Context(), "hooks", "hook")
	require.NoError(t, err)
	assert.Nil(t, data, "a cleared node reads as no image")
}

func TestFlowsServiceNodeImageErrorKinds(t *testing.T) {
	flows := flow.NewFlowStore(t.TempDir(), nil)
	require.NoError(t, flows.Save(webhookServiceFlow()))
	service := newFlowsService(flows, nil, seededCreds(t), testImages(t), testMarks(t), testScripts(), nil)

	_, err := service.SetNodeImage(t.Context(), "nope", "hook", pngBytes(t))
	assert.Equal(t, KindNotFound, KindOf(err), "unknown flow is not-found")

	_, err = service.SetNodeImage(t.Context(), "hooks", "ghost", pngBytes(t))
	assert.Equal(t, KindNotFound, KindOf(err), "unknown node is not-found")

	_, err = service.SetNodeImage(t.Context(), "hooks", "inbox", pngBytes(t))
	assert.Equal(t, KindInvalid, KindOf(err), "a non-webhook node cannot carry an image")

	_, err = service.SetNodeImage(t.Context(), "hooks", "hook", []byte("not an image"))
	assert.Equal(t, KindInvalid, KindOf(err), "an undecodable image is rejected")
}

func TestFlowsServiceDeleteFlowPurgesPipelineStateAndRetriesMissingFiles(t *testing.T) {
	db, err := store.Open(t.Context(), t.TempDir(), store.DefaultOpenOptions())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	flows := flow.NewFlowStore(t.TempDir(), nil)
	service := newFlowsService(flows, db, seededCreds(t), testImages(t), testMarks(t), testScripts(), nil)
	created, err := service.Create(t.Context(), "Profile")
	require.NoError(t, err)
	_, err = db.Queries().InsertInboxItem(t.Context(), store.InsertInboxItemParams{
		ProfileID: created.ID, SourceKind: "github", ExternalID: "item", Payload: []byte(`{}`), Lifecycle: "active",
	})
	require.NoError(t, err)
	_, err = db.Append(t.Context(), "source:"+created.ID+"/source", "item", []byte(`{}`))
	require.NoError(t, err)

	require.NoError(t, service.Delete(t.Context(), created.ID))
	// The second call is the files-first retry path: the yaml file is already
	// gone, but PurgeProfile remains an idempotent no-op.
	require.NoError(t, service.Delete(t.Context(), created.ID))
	for _, table := range []string{"inbox_item", "event_log", "consumer_offset", "source_head"} {
		var count int
		require.NoError(t, db.Conn().QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&count))
		assert.Zero(t, count, table)
	}
}

func TestFlowsServiceCreateSeedsWithTheOneConnectedAccount(t *testing.T) {
	flows := flow.NewFlowStore(t.TempDir(), nil)
	service := newFlowsService(flows, nil, seededCreds(t), testImages(t), testMarks(t), testScripts(), nil)

	created, err := service.Create(t.Context(), "Triage")
	require.NoError(t, err)
	assert.NotEmpty(t, created.Nodes, "one connected account is enough to seed the starter graph")
	assert.NotEmpty(t, flows.GetLayout(created.ID).Nodes, "the seeded nodes need canvas positions")
}

// A workspace is the thing that exists without any credential: first run
// creates it before it offers to connect anything, so an unseeded create is
// the expected path there rather than a failure.
func TestFlowsServiceCreateWithoutAnUnambiguousAccountMakesAnEmptyWorkspace(t *testing.T) {
	for _, tc := range []struct {
		name string
		refs []string
	}{
		{"no account connected", nil},
		{"several accounts connected", []string{"github/octocat", "github/hubot"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			creds := credentials.NewMemoryStore()
			for _, raw := range tc.refs {
				ref, err := credentials.ParseRef(raw)
				require.NoError(t, err)
				require.NoError(t, creds.Set(ref, "token"))
			}
			service := newFlowsService(flow.NewFlowStore(t.TempDir(), nil), nil, creds, testImages(t), testMarks(t), testScripts(), nil)

			created, err := service.Create(t.Context(), "Triage")
			require.NoError(t, err)
			assert.Empty(t, created.Nodes)
		})
	}
}

func TestFlowsServiceSeedStarterFillsAnEmptyWorkspace(t *testing.T) {
	flows := flow.NewFlowStore(t.TempDir(), nil)
	creds := credentials.NewMemoryStore()
	updates := 0
	service := newFlowsService(flows, nil, creds, testImages(t), testMarks(t), testScripts(), func() { updates++ })

	// The first-run order: the workspace exists before the account does.
	created, err := service.Create(t.Context(), "Triage")
	require.NoError(t, err)
	require.Empty(t, created.Nodes)

	_, err = service.SeedStarter(t.Context(), created.ID)
	require.ErrorContains(t, err, "Connect exactly one GitHub account")

	ref, err := credentials.ParseRef(seedRef)
	require.NoError(t, err)
	require.NoError(t, creds.Set(ref, "token"))

	seeded, err := service.SeedStarter(t.Context(), created.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, seeded.Nodes)
	assert.NotEmpty(t, flows.GetLayout(created.ID).Nodes)
	assert.Equal(t, created.Name, seeded.Name, "seeding keeps the name the user chose")
	assert.Equal(t, 2, updates, "create and seed each reshape the flows list")

	// Appending a second starter graph onto a graph someone has since edited
	// is not a mistake they can undo, so a populated workspace is refused.
	_, err = service.SeedStarter(t.Context(), created.ID)
	require.ErrorContains(t, err, "already has nodes")

	_, err = service.SeedStarter(t.Context(), "missing")
	require.ErrorContains(t, err, "not found")
}

func TestFlowsServiceSetFlowEnabled(t *testing.T) {
	flows := flow.NewFlowStore(t.TempDir(), nil)
	created, err := flows.Create("Triage", starterSeed(seedRef))
	require.NoError(t, err)

	updates := 0
	service := newFlowsService(flows, nil, seededCreds(t), testImages(t), testMarks(t), testScripts(), func() { updates++ })
	summary, err := service.SetEnabled(t.Context(), created.ID, false)
	require.NoError(t, err)
	assert.Equal(t, created.ID, summary.ID)
	assert.False(t, summary.Enabled)
	assert.Equal(t, 1, updates)

	stored, ok := flows.Get(created.ID)
	require.True(t, ok)
	assert.False(t, stored.Enabled)
}

func TestFlowsServiceSetFlowEnabledDoesNotEmitOnFailure(t *testing.T) {
	updates := 0
	service := newFlowsService(flow.NewFlowStore(t.TempDir(), nil), nil, seededCreds(t), testImages(t), testMarks(t), testScripts(), func() { updates++ })

	_, err := service.SetEnabled(t.Context(), "missing", false)
	require.Error(t, err)
	assert.Zero(t, updates)
}

func TestFlowsServiceProfileImageLifecycle(t *testing.T) {
	flows := flow.NewFlowStore(t.TempDir(), nil)
	created, err := flows.Create("Triage", starterSeed(seedRef))
	require.NoError(t, err)

	updates := 0
	service := newFlowsService(flows, nil, seededCreds(t), testImages(t), testMarks(t), testScripts(), func() { updates++ })

	set, err := service.SetProfileImage(t.Context(), created.ID, pngBytes(t))
	require.NoError(t, err)
	require.NotEmpty(t, set.Image, "the flow records a content-hash reference")
	assert.Equal(t, 1, updates)

	data, err := service.ProfileImage(t.Context(), created.ID)
	require.NoError(t, err)
	require.NotEmpty(t, data)
	img, err := png.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	assert.Equal(t, image.Rect(0, 0, 128, 128), img.Bounds(), "stored avatar is a 128px square PNG")

	// A graph save carries no image field; the store must preserve the avatar
	// rather than drop it.
	editable, ok := flows.Get(created.ID)
	require.True(t, ok)
	editable.Image = ""
	require.NoError(t, service.Save(t.Context(), editable))
	preserved, err := service.ProfileImage(t.Context(), created.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, preserved, "a graph save preserves the image it does not carry")
	stored, ok := flows.Get(created.ID)
	require.True(t, ok)
	assert.Equal(t, set.Image, stored.Image, "the reference survives a graph save")

	cleared, err := service.ClearProfileImage(t.Context(), created.ID)
	require.NoError(t, err)
	assert.Empty(t, cleared.Image)
	gone, err := service.ProfileImage(t.Context(), created.ID)
	require.NoError(t, err)
	assert.Empty(t, gone, "clearing removes the stored bytes")
}

func TestFlowsServiceSetProfileImageRejectsBadInput(t *testing.T) {
	flows := flow.NewFlowStore(t.TempDir(), nil)
	created, err := flows.Create("Triage", starterSeed(seedRef))
	require.NoError(t, err)
	service := newFlowsService(flows, nil, seededCreds(t), testImages(t), testMarks(t), testScripts(), nil)

	_, err = service.SetProfileImage(t.Context(), created.ID, []byte("not an image"))
	require.Error(t, err)
	assert.Equal(t, KindInvalid, KindOf(err))
	stored, ok := flows.Get(created.ID)
	require.True(t, ok)
	assert.Empty(t, stored.Image, "a rejected image leaves the flow unreferenced")

	_, err = service.SetProfileImage(t.Context(), "missing", pngBytes(t))
	require.Error(t, err)
	assert.Equal(t, KindNotFound, KindOf(err))
}
