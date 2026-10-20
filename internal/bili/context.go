package bili

import "context"

// contextBackground 用于内部不发散取消的后台调用（如签名素材缓存刷新）。
func contextBackground() context.Context { return context.Background() }
