package mediaapp

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/media"
)

func fakeCollection(t *testing.T) (*Collection, *[]*fakePlayback, *[]media.Options) {
	t.Helper()
	c := NewCollection(media.Options{AudioOutput: "null"})
	var players []*fakePlayback
	var options []media.Options
	c.factory = func(o media.Options) (playback, error) {
		player := &fakePlayback{state: media.State{Loaded: true, HasVideo: true, Duration: 120, Volume: 70, Width: 32, Height: 24}}
		if o.InitialState != nil {
			player.state.Paused = o.InitialState.Paused
			player.state.Volume = o.InitialState.Volume
			player.state.Muted = o.InitialState.Muted
		}
		players = append(players, player)
		options = append(options, o)
		return player, nil
	}
	t.Cleanup(func() { c.Close() })
	return c, &players, &options
}

func TestVideoCollectionIndependentInputPlaybackAndTextureLifetimes(t *testing.T) {
	c, players, _ := fakeCollection(t)
	first, second := mediaFile(t), mediaFile(t)
	for i, file := range []*os.File{first, second} {
		if key, err := c.OpenFile(file, "movie.mp4"); err != nil || key != mediaSlotKey(i) {
			t.Fatal(key, err)
		}
	}
	if err := c.Poll(); err != nil {
		t.Fatal(err)
	}
	ids := []uint64{c.Surfaces()[0].ID, c.Surfaces()[1].ID}
	textures := []uint64{c.Surfaces()[0].Texture.ID(), c.Surfaces()[1].Texture.ID()}
	if ids[0] == ids[1] || textures[0] == textures[1] {
		t.Fatal("independent players share identity/resources")
	}
	c.Focus(ids[0])
	c.Send(ids[0], experience.Event{Kind: experience.KeyInput, Keycode: 57, Pressed: true})
	if !(*players)[0].state.Paused || (*players)[1].state.Paused {
		t.Fatal("pause affected wrong player")
	}
	c.Focus(ids[1])
	c.Send(ids[0], experience.Event{Kind: experience.KeyInput, Keycode: 57, Pressed: true})
	if !(*players)[0].state.Paused {
		t.Fatal("unfocused player consumed typing")
	}
	c.Send(ids[1], experience.Event{Kind: experience.KeyInput, Keycode: 106, Pressed: true})
	if (*players)[1].state.Position != 10 || (*players)[0].state.Position != 0 {
		t.Fatal("seek affected wrong player")
	}
	c.Resize(ids[1], 800, 500)
	if c.slots[0].manager.renderer.image.Bounds() == c.slots[1].manager.renderer.image.Bounds() {
		t.Fatal("resize affected both players")
	}
	if err := c.Poll(); err != nil {
		t.Fatal(err)
	}
	saved := c.SessionStates()
	if len(saved) != 2 || !saved[0].Paused || saved[1].Position != 10 {
		t.Fatal("per-window playback settings were not captured", saved)
	}
	c.CloseApplication(ids[0])
	if !(*players)[0].closed || (*players)[1].closed || len(c.Surfaces()) != 1 || c.Surfaces()[0].ID != ids[1] {
		t.Fatal("closing one player closed sibling")
	}
	if _, err := first.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("closed player retained descriptor", err)
	}
	if _, err := second.Stat(); err != nil {
		t.Fatal("sibling descriptor was closed", err)
	}
	if retired := c.RetiredTextures(); !reflect.DeepEqual(retired, []uint64{textures[0]}) || len(c.RetiredTextures()) != 0 {
		t.Fatal("wrong texture retirement", retired)
	}
	if key, err := c.OpenFile(mediaFile(t), "third.mp4"); err != nil || key != mediaSlotKey(0) {
		t.Fatal(key, err)
	}
	replacement := c.Surfaces()[0].ID
	if replacement <= ids[1] {
		t.Fatal("slot reused old runtime identity")
	}
	c.CloseApplication(ids[0])
	c.Resize(ids[0], 900, 600)
	c.Focus(ids[0])
	c.Send(ids[0], experience.Event{Kind: experience.KeyInput, Keycode: 57, Pressed: true})
	if c.slots[0] == nil || c.slots[0].manager.focused || (*players)[2].closed || (*players)[2].state.Paused {
		t.Fatal("stale player identity controlled replacement")
	}
	c.Close()
	if len(c.Surfaces()) != 0 || c.SessionStates() != nil || len(c.RetiredTextures()) != 2 {
		t.Fatal("collection close leaked resources")
	}
	if err := c.Close(); err != nil || len(c.RetiredTextures()) != 0 {
		t.Fatal("repeated close/retirement was not idempotent")
	}
}

func TestVideoCollectionRestoresSparseSlotsAndPlaybackBeforeUnpausing(t *testing.T) {
	c, players, options := fakeCollection(t)
	file := mediaFile(t)
	state := WindowState{Key: "native:media-player-4", SessionState: SessionState{Path: file.Name(), Position: 37, Paused: false, Volume: 24, Muted: true}}
	if key, err := c.RestoreFile(file, "restored.mp4", state); err != nil || key != state.Key {
		t.Fatal(key, err)
	}
	if len(*options) != 1 || (*options)[0].InitialState == nil || !(*options)[0].InitialState.Paused || (*options)[0].InitialState.Volume != 24 || !(*options)[0].InitialState.Muted {
		t.Fatal("restore exposed default playing audio")
	}
	if key, err := c.NextKey(); err != nil || key != mediaSlotKey(0) {
		t.Fatal("sparse restore renumbered slots", key, err)
	}
	if saved := c.SessionStates(); len(saved) != 1 || saved[0] != state {
		t.Fatal("loading restore lost requested playback state", saved)
	}
	if err := c.Poll(); err != nil {
		t.Fatal(err)
	}
	if !(*players)[0].state.Paused || !reflect.DeepEqual((*players)[0].seeks, []float64{37}) {
		t.Fatal("restore played before first seek")
	}
	if err := c.Poll(); err != nil {
		t.Fatal(err)
	}
	if (*players)[0].state.Paused || (*players)[0].state.Position != 37 {
		t.Fatal("restore did not apply its playback state after seek")
	}
	data, err := json.Marshal(c.SessionStates())
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip []WindowState
	if err = json.Unmarshal(data, &roundtrip); err != nil || len(roundtrip) != 1 || roundtrip[0] != state || roundtrip[0].Validate() != nil {
		t.Fatal("window session JSON did not round trip", roundtrip, err)
	}
	duplicate := mediaFile(t)
	if _, err = c.RestoreFile(duplicate, "replacement", state); err == nil {
		t.Fatal("restore replaced an occupied slot")
	}
	if _, err = duplicate.Stat(); err != nil {
		t.Fatal("failed restore consumed descriptor", err)
	}
	if len(*players) != 1 {
		t.Fatal("failed duplicate restore started another decoder")
	}
	for _, key := range []string{"", "native:media-player-1", "native:media-player-04", "native:media-player-5", "native:photo-viewer"} {
		invalid := state
		invalid.Key = key
		if invalid.Validate() == nil {
			t.Fatal("accepted invalid media slot", key)
		}
	}
}

func TestVideoCollectionCapacityAndFactoryFailurePreserveCurrentPlayers(t *testing.T) {
	c, players, _ := fakeCollection(t)
	for i := 0; i < MaxViewers; i++ {
		if _, err := c.OpenFile(mediaFile(t), "movie.mp4"); err != nil {
			t.Fatal(err)
		}
	}
	file := mediaFile(t)
	if _, err := c.OpenFile(file, "overflow.mp4"); err == nil {
		t.Fatal("video collection exceeded capacity")
	}
	if _, err := file.Stat(); err != nil {
		t.Fatal("capacity rejection consumed caller file")
	}
	if len(*players) != MaxViewers {
		t.Fatal("capacity check started an extra decoder")
	}
	firstID := c.Surfaces()[0].ID
	c.CloseApplication(firstID)
	c.factory = func(media.Options) (playback, error) { return nil, errors.New("decoder unavailable") }
	if _, err := c.OpenFile(file, "unavailable.mp4"); err == nil {
		t.Fatal("factory failure did not propagate")
	}
	if _, err := file.Stat(); err != nil {
		t.Fatal("factory failure consumed caller file")
	}
	if key, err := c.NextKey(); err != nil || key != mediaSlotKey(0) {
		t.Fatal("failed creation consumed stable slot", key, err)
	}
	for _, player := range (*players)[1:] {
		if player.closed {
			t.Fatal("failed creation closed another player")
		}
	}
	c.Close()
	if _, err := c.OpenFile(file, "closed.mp4"); err == nil {
		t.Fatal("closed collection accepted video")
	}
}
