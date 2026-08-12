本次工作区新增的是 M7「性能工程与热点房间保护」，核心功能包括：
热点弹幕治理：按在线人数或弹幕速率进入 NORMAL / HOT / PROTECT；HOT 默认保留 50%，PROTECT 保留 20%，未保留的弹幕不实时广播，但仍走异步持久化。采样基于消息 ID 哈希，结果稳定可复现。[策略实现 (line 1)](/home/xin/work/live-platform/internal/traffic/policy.go:1)

新增热点阈值与采样比例环境变量，并在启动时做合法性校验。[配置 (line 50)](/home/xin/work/live-platform/internal/config/config.go:50)

弹幕响应和实时事件加入 traffic_mode、broadcasted，并标注消息优先级：礼物 P1、弹幕 P3、统计 P4。[弹幕 (line 48)](/home/xin/work/live-platform/internal/danmaku/service.go:48)

慢消费者保护：Centrifugo 单客户端发送队列限制为 256 KiB，避免积压无限增长。[配置 (line 1)](/home/xin/work/live-platform/configs/centrifugo.json:1)

可观测性：新增 live_danmaku_degradation_total{mode,action} 指标与 Grafana「Danmaku degradation decisions」面板。[指标 (line 16)](/home/xin/work/live-platform/internal/observability/metrics.go:16)

压测能力：
WebSocket 压测器：连接/订阅、单热点 vs 多房间、服务端广播、端到端 P50/P95/P99、断线恢复、慢消费者模拟。[tools/loadtest (line 101)](/home/xin/work/live-platform/tools/loadtest/main.go:101)
HTTP 压测器：Like/Gift 的限速、并发、幂等键、状态码和延迟统计。[tools/httpload (line 27)](/home/xin/work/live-platform/tools/httpload/main.go:27)
新增连接阶梯、热点房、慢消费者、Like/Gift、Soak、故障注入、运行快照等 make m7-* 脚本。[Makefile (line 48)](/home/xin/work/live-platform/Makefile:48)

补充 M7 基准测试报告模板与容量计算口径，明确不会预填未经压测验证的容量数据。[README (line 65)](/home/xin/work/live-platform/README.md:65)