package sync

import (
	"github.com/sheyiyuan/zero-beat/internal/config"
	"github.com/sheyiyuan/zero-beat/internal/store"
)

// DefaultOptions 用应用数据目录构造同步选项；revision 由 app.Service 提供。
func DefaultOptions(st *store.Store, revision func() uint64) (Options, error) {
	dir, err := config.SyncDir()
	if err != nil {
		return Options{}, err
	}
	cfgPath, err := config.SyncTomlPath()
	if err != nil {
		return Options{}, err
	}
	keyPath, err := config.SyncKeyPath()
	if err != nil {
		return Options{}, err
	}
	tokenPath, err := config.SyncTokenPath()
	if err != nil {
		return Options{}, err
	}
	keymapPath, err := config.KeymapPath()
	if err != nil {
		return Options{}, err
	}
	return Options{
		Store:      st,
		Dir:        dir,
		ConfigPath: cfgPath,
		KeyPath:    keyPath,
		TokenPath:  tokenPath,
		KeymapPath: keymapPath,
		Revision:   revision,
	}, nil
}
