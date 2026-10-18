// Package bilibili 实现 B 站音源。
//
// 计划从 v1 的 internal/services 搬迁：二维码登录、BV 解析、多 P 分集、
// playurl 取流、收藏夹同步。搬迁时顺手修掉两个问题：
//
//  1. 选轨取 dash.audio[0] 会拿到最低码率的 HE-AAC 轨，改为按带宽降序
//     并优先选择 native 可解的编码（mp4a.40.2）；
//  2. 播放直链不落库，改为每次解析并携带过期时间。
//
// TODO(P1): 落地 Source 接口，先跑通 Resolve。
package bilibili
