//go:build !linux

package sysmedia

// New 在尚未实现系统媒体集成的平台返回空服务，调用方按 nil 处理。
func New() (Service, error) { return nil, nil }
