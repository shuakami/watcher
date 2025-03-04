# Watcher

`Watcher` 是一个用于监控文件系统变更并生成版本化快照的 Go 包。

## 特性

- **递归监控**：自动捕获指定路径下文件和目录的增删改事件。
- **事件合并**：通过 Debounce 减少事件风暴。
- **并发处理**：使用 worker 池并发处理文件变更。
- **快照管理**：每次变更时自动生成新快照，并维护快照的有向无环图（DAG）。
- **文件元信息**：为每个快照记录文件的大小、修改时间、哈希等信息。
- **事件通知**：通过事件通道向外部暴露文件变更事件。
- **并发安全**：使用 sync.RWMutex 保证并发访问安全。

## 安装

```bash
go get github.com/shuakami/watcher
```

## 快速开始

```go
package main

import (
	"fmt"
	"github.com/shuakami/watcher"
	"time"
)

func main() {
	cfg := watcher.ConfigWatcher{
		WatchPaths:     []string{"/path/to/watch"},
		IgnorePatterns: []string{"*.tmp"},
		Debounce:       50 * time.Millisecond,
		WorkerCount:    4,
	}

	w, err := watcher.NewWatcher(cfg)
	if err != nil {
		fmt.Println("Error creating watcher:", err)
		return
	}

	if err := w.Start(); err != nil {
		fmt.Println("Error starting watcher:", err)
		return
	}

	// 处理事件
	go func() {
		for evt := range w.EventChan {
			fmt.Printf("Event: %s %s\n", evt.Op.String(), evt.FilePath)
		}
	}()

	// 运行一段时间后停止
	time.Sleep(10 * time.Second)
	w.Stop()
}
```

## 文档

详细文档请参阅 [GoDoc](https://pkg.go.dev/github.com/shuakami/watcher)

## 贡献

欢迎提交 Issue 和 Pull Request！

## 许可证

本项目采用 [GNU General Public License v3.0](LICENSE) 开源许可证。