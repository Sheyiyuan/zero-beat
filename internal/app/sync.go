package app

import (
	"encoding/json"
	"sync/atomic"

	"github.com/sheyiyuan/zero-beat/internal/source"
)

// libraryRevision 是进程内「歌单/登录态被修改」的计数器。
//
// 它刻意放在包级而不是 Service 字段里：tui 每个 tea.Cmd 都会新建一个
// app.Service，字段级计数器无法跨命令比较。同步管理器只需要一个单调递增的
// 信号来判断「本轮期间用户是否改过库」，不需要精确值。
var libraryRevision atomic.Uint64

// LibraryRevision 返回当前修改计数；同步在导出前记录、导入前比较。
func LibraryRevision() uint64 { return libraryRevision.Load() }

func bumpLibraryRevision() { libraryRevision.Add(1) }

// ApplyStoredSessions 把数据库里的登录态回填到各音源。
// 同步拉取到较新的登录态后调用；返回实际回填的音源。
func (s *Service) ApplyStoredSessions() []source.ID {
	if s.sources == nil || s.store == nil {
		return nil
	}
	var out []source.ID
	for _, src := range s.sources.All() {
		payload, _, err := s.store.LoadAccountAt(string(src.ID()))
		if err != nil || len(payload) == 0 {
			continue
		}
		var session map[string]string
		if err := json.Unmarshal(payload, &session); err != nil || len(session) == 0 {
			continue
		}
		src.SetSession(session)
		out = append(out, src.ID())
	}
	return out
}
