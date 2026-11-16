package keymap

import (
	tea "charm.land/bubbletea/v2"
	core "github.com/sheyiyuan/zero-beat/zero-beat-core/keymap"
)

type Action = core.Action
type Keymap struct{ *core.Keymap }

var Defaults = core.Defaults

const (
	Quit             = core.Quit
	HelpPopup        = core.HelpPopup
	HelpPage         = core.HelpPage
	Login            = core.Login
	LocalSearch      = core.LocalSearch
	Search           = core.Search
	QueueView        = core.QueueView
	LyricsView       = core.LyricsView
	PlayPause        = core.PlayPause
	Next             = core.Next
	Previous         = core.Previous
	SeekBack1        = core.SeekBack1
	SeekBack5        = core.SeekBack5
	SeekFwd5         = core.SeekFwd5
	SeekFwd10        = core.SeekFwd10
	VolumeDown       = core.VolumeDown
	VolumeUp         = core.VolumeUp
	CompDown         = core.CompDown
	CompUp           = core.CompUp
	SongCompDown     = core.SongCompDown
	SongCompUp       = core.SongCompUp
	Mode             = core.Mode
	Mute             = core.Mute
	Quality          = core.Quality
	Sleep            = core.Sleep
	OffsetDown       = core.OffsetDown
	OffsetUp         = core.OffsetUp
	OffsetReset      = core.OffsetReset
	Up               = core.Up
	Down             = core.Down
	Left             = core.Left
	Right            = core.Right
	Top              = core.Top
	Bottom           = core.Bottom
	PageUp           = core.PageUp
	PageDown         = core.PageDown
	PlaySelected     = core.PlaySelected
	Select           = core.Select
	SelectAll        = core.SelectAll
	Download         = core.Download
	DownloadSelected = core.DownloadSelected
	AddPlaylist      = core.AddPlaylist
	MergePlaylist    = core.MergePlaylist
	Enqueue          = core.Enqueue
	PlayNext         = core.PlayNext
	Remove           = core.Remove
	Delete           = core.Delete
	Rename           = core.Rename
	New              = core.New
	Folders          = core.Folders
	Downloads        = core.Downloads
	MixEdit          = core.MixEdit
	EditClip         = core.EditClip
	BindLyrics       = core.BindLyrics
	ClipSetStart     = core.ClipSetStart
	ClipSetEnd       = core.ClipSetEnd
	ClipClear        = core.ClipClear
	Unlock           = core.Unlock
	Refresh          = core.Refresh
	QueueUp          = core.QueueUp
	QueueDown        = core.QueueDown
	Source           = core.Source
	LoginImage       = core.LoginImage
	Logout           = core.Logout
	Back             = core.Back
	Sync             = core.Sync
)

func Default() *Keymap                  { return &Keymap{core.Default()} }
func Load(path string) (*Keymap, error) { k, _, e := LoadWithWarnings(path); return k, e }
func LoadWithWarnings(path string) (*Keymap, []string, error) {
	k, w, e := core.LoadWithWarnings(path)
	if e != nil {
		return nil, w, e
	}
	return &Keymap{k}, w, nil
}
func ExampleTOML() string { return core.ExampleTOML() }
func (k *Keymap) Match(a Action, key tea.Key) bool {
	if k == nil {
		return false
	}
	return k.Keymap.Match(a, fromTea(key))
}
func fromTea(k tea.Key) core.Key {
	out := core.Key{Code: k.Code, Text: k.Text}
	switch k.Code {
	case tea.KeySpace:
		out.Code = core.KeySpace
	case tea.KeyEnter:
		out.Code = core.KeyEnter
	case tea.KeyTab:
		out.Code = core.KeyTab
	case tea.KeyEscape:
		out.Code = core.KeyEscape
	case tea.KeyBackspace:
		out.Code = core.KeyBackspace
	case tea.KeyDelete:
		out.Code = core.KeyDelete
	case tea.KeyUp:
		out.Code = core.KeyUp
	case tea.KeyDown:
		out.Code = core.KeyDown
	case tea.KeyLeft:
		out.Code = core.KeyLeft
	case tea.KeyRight:
		out.Code = core.KeyRight
	case tea.KeyPgUp:
		out.Code = core.KeyPgUp
	case tea.KeyPgDown:
		out.Code = core.KeyPgDown
	case tea.KeyHome:
		out.Code = core.KeyHome
	case tea.KeyEnd:
		out.Code = core.KeyEnd
	}
	if k.Mod&tea.ModCtrl != 0 {
		out.Mod |= core.ModCtrl
	}
	if k.Mod&tea.ModAlt != 0 {
		out.Mod |= core.ModAlt
	}
	if k.Mod&tea.ModShift != 0 {
		out.Mod |= core.ModShift
	}
	if k.Mod&tea.ModSuper != 0 {
		out.Mod |= core.ModSuper
	}
	return out
}
func ParseKey(raw string) (tea.Key, error) {
	k, e := core.ParseKey(raw)
	if e != nil {
		return tea.Key{}, e
	}
	out := tea.Key{Code: k.Code, Text: k.Text}
	switch k.Code {
	case core.KeySpace:
		out.Code = tea.KeySpace
	case core.KeyEnter:
		out.Code = tea.KeyEnter
	case core.KeyTab:
		out.Code = tea.KeyTab
	case core.KeyEscape:
		out.Code = tea.KeyEscape
	case core.KeyBackspace:
		out.Code = tea.KeyBackspace
	case core.KeyDelete:
		out.Code = tea.KeyDelete
	case core.KeyUp:
		out.Code = tea.KeyUp
	case core.KeyDown:
		out.Code = tea.KeyDown
	case core.KeyLeft:
		out.Code = tea.KeyLeft
	case core.KeyRight:
		out.Code = tea.KeyRight
	case core.KeyPgUp:
		out.Code = tea.KeyPgUp
	case core.KeyPgDown:
		out.Code = tea.KeyPgDown
	case core.KeyHome:
		out.Code = tea.KeyHome
	case core.KeyEnd:
		out.Code = tea.KeyEnd
	}
	if k.Mod&core.ModCtrl != 0 {
		out.Mod |= tea.ModCtrl
	}
	if k.Mod&core.ModAlt != 0 {
		out.Mod |= tea.ModAlt
	}
	if k.Mod&core.ModShift != 0 {
		out.Mod |= tea.ModShift
	}
	if k.Mod&core.ModSuper != 0 {
		out.Mod |= tea.ModSuper
	}
	return out, nil
}
