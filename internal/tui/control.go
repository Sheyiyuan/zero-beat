package tui

import (
	"encoding/json"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/sheyiyuan/zero-beat/internal/control"
	"github.com/sheyiyuan/zero-beat/internal/keymap"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

// controlDenied 是控制通道默认拒绝的动作：会删文件、改歌单或动远端仓库。
// 按动作而不是按字面键判断，因为 keymap.toml 允许用户重绑。
var controlDenied = []keymap.Action{
	keymap.Quit,
	keymap.Delete,
	keymap.Remove,
	keymap.Download,
	keymap.DownloadSelected,
	keymap.AddPlaylist,
	keymap.MergePlaylist,
	keymap.New,
	keymap.Rename,
	keymap.Unlock,
	keymap.Logout,
	keymap.Sync,
}

// controlSong 是帧快照里的曲目信息。
type controlSong struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Source string `json:"source"`
}

// controlState 是 `zero frame --json` 输出的状态快照。
type controlState struct {
	View       string       `json:"view"`
	Width      int          `json:"width"`
	Height     int          `json:"height"`
	Playlist   string       `json:"playlist,omitempty"`
	Selected   *controlSong `json:"selected,omitempty"`
	Current    *controlSong `json:"current,omitempty"`
	State      string       `json:"state"`
	PositionMS int64        `json:"position_ms"`
	DurationMS int64        `json:"duration_ms"`
	Volume     float64      `json:"volume"`
	Muted      bool         `json:"muted"`
	Mode       string       `json:"mode"`
	Queue      int          `json:"queue"`
	Notice     string       `json:"notice,omitempty"`
}

func songInfo(song store.Song) *controlSong {
	return &controlSong{
		ID:     song.ID,
		Title:  song.Title,
		Artist: song.Artist,
		Source: string(song.Source),
	}
}

// controlSnapshot 只在事件循环 goroutine 上调用，所以可以直接读模型字段。
func (m *Model) controlSnapshot() controlState {
	s := controlState{
		Width:      m.width,
		Height:     m.height,
		State:      m.state.String(),
		PositionMS: m.position.Milliseconds(),
		DurationMS: m.duration.Milliseconds(),
		Volume:     m.volume,
		Muted:      m.muted,
		Mode:       m.mode.String(),
		Queue:      len(m.queue),
		Notice:     m.note.text,
	}
	if int(m.view) >= 0 && int(m.view) < len(viewNames) {
		s.View = m.viewTitle()
	}
	if m.playlistIdx >= 0 && m.playlistIdx < len(m.playlists) {
		s.Playlist = m.playlists[m.playlistIdx].Title
	}
	if m.current != nil {
		s.Current = songInfo(*m.current)
	}
	switch m.view {
	case viewQueue:
		if m.queueCursor >= 0 && m.queueCursor < len(m.queue) {
			s.Selected = songInfo(m.queue[m.queueCursor])
		}
	case viewSearch:
		// 搜索页的焦点在搜索结果上，和曲库的 songIdx 不是一回事。
		switch {
		case m.searchMode == searchSource && m.searchIdx >= 0 && m.searchIdx < len(m.searchResults):
			s.Selected = songInfo(m.searchResultSong(m.searchIdx))
		case m.searchMode != searchSource && m.searchIdx >= 0 && m.searchIdx < len(m.localResults):
			s.Selected = songInfo(m.localResults[m.searchIdx])
		}
	case viewLibrary, viewLyrics:
		if m.songIdx >= 0 && m.songIdx < len(m.songs) {
			s.Selected = songInfo(m.songs[m.songIdx])
		}
	}
	return s
}

// textInputFocused 报告当前是否有文本框会消费字符；判断顺序与 handleKey 的路由一致。
//
// 控制通道的 +type 只在有输入态时放行：否则字符会落进全局快捷键，
// 例如 +type q 会直接退出，绕过 deniedControl。
func (m *Model) textInputFocused() bool {
	switch {
	case m.menu != nil:
		return false
	case m.palette != nil:
		return true
	case m.prompt != promptNone:
		return true
	case m.clip != nil:
		return false
	case m.picker != nil:
		return true
	case m.helpPopup, m.aboutPopup:
		return false
	case m.view == viewSearch && m.searchEditing:
		return true
	case m.view == viewSync:
		// 同步页只有已经进入编辑态才保证字符都进输入缓冲；
		// 未进入编辑态时，部分字符会被当成导航键。
		return m.syncEdit != ""
	}
	return false
}

// deniedControl 返回该按键命中的受限动作；未命中时第二个返回值为 false。
func (m *Model) deniedControl(key tea.Key) (keymap.Action, bool) {
	// ctrl+c 是硬编码的退出键，不走 keymap，单独拦。
	if isCtrl(key, 'c') {
		return keymap.Quit, true
	}
	km := m.keys
	if km == nil {
		km = defaultKeys
	}
	for _, a := range controlDenied {
		if km.Match(a, key) {
			return a, true
		}
	}
	return "", false
}

// handleControlRequest 处理控制通道请求；只在事件循环内调用。
func (m *Model) handleControlRequest(req control.Request) (tea.Model, tea.Cmd) {
	reply := control.Reply{}
	var cmd tea.Cmd

	switch req.Kind {
	case control.KindFrame:
	case control.KindKey:
		key, err := keymap.ParseKey(req.Key)
		if err != nil {
			reply.Err = err.Error()
			break
		}
		if action, denied := m.deniedControl(key); denied && !req.Force {
			reply.Err = fmt.Sprintf("按键 %s 会执行 %s，可能改动数据；确需执行请加 --force", req.Key, action)
			break
		}
		next, c := m.handleKey(key)
		cmd = c
		if nm, ok := next.(*Model); ok {
			m = nm
		}
	case control.KindType:
		if !m.textInputFocused() {
			reply.Err = "当前没有输入态；先用 +<按键> 打开搜索、命令面板或输入框，再重试"
			break
		}
		cmds := make([]tea.Cmd, 0, len([]rune(req.Text)))
		for _, r := range req.Text {
			next, c := m.handleKey(tea.Key{Code: r, Text: string(r)})
			if c != nil {
				cmds = append(cmds, c)
			}
			if nm, ok := next.(*Model); ok {
				m = nm
			}
		}
		cmd = tea.Batch(cmds...)
	default:
		reply.Err = fmt.Sprintf("未知控制请求 %q", req.Kind)
	}

	if reply.Err == "" {
		reply.Frame = m.View().Content
		if data, err := json.Marshal(m.controlSnapshot()); err == nil {
			reply.State = data
		}
	}
	// Reply 由服务端以缓冲通道创建；非阻塞发送避免调用方超时后把这里堵死。
	select {
	case req.Reply <- reply:
	default:
	}
	return m, cmd
}
