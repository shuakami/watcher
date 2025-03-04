// Package watcher 提供文件系统的变更监控与版本化快照管理功能。
//
// 核心特点：
//   - 递归监控指定路径，自动捕获文件/目录的增删改事件
//   - 通过Debounce（事件合并）减少过多的事件风暴
//   - 采用worker池并发处理文件变更
//   - 每次检测到变更时自动生成新快照（SnapshotNode），并维护DAG
//   - 为每个快照记录文件元信息（大小、修改时间、哈希等）
//   - 允许外部通过EventChan接收变更事件
//   - 使用sync.RWMutex保证并发访问安全
//   - 提供可定制的忽略规则（IgnorePatterns）
//
// 注意：
//   - Windows、Linux、macOS等不同平台对文件系统事件的支持存在差异
//   - 大量文件频繁变更时，可能需要调大通道buffer或优化Debounce
//   - 目录的哈希暂未实现，仅对文件内容做哈希校验
//   - Stop() 方法会关闭所有后台goroutine，并在退出前flush一次事件
//
// 推荐使用方式：
//  1. 配置ConfigWatcher
//  2. 通过NewWatcher创建Watcher
//  3. 调用Start()开始监控
//  4. 通过EventChan或ListAllSnapshots()等方法获取监控结果
//  5. 调用Stop()结束监控
//
// 并发安全：
//   - 除了EventChan外，其它对内部状态的读写都使用互斥锁（sync.RWMutex）保护
//   - 同一时刻允许多个读取操作，但写操作（创建新快照）是排他的
//   - 对外暴露的API（GetCurrentSnapshot、GetSnapshotByID等）在并发调用时是安全的
//
// 使用限制：
//   - 需要在环境中安装fsnotify依赖: go get github.com/fsnotify/fsnotify
//   - 如果要在容器中使用，需要保证宿主机和容器的文件系统事件转发正常
package watcher
