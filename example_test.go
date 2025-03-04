package watcher

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ExampleWatcher 展示最简使用场景
//
// 运行示例命令: go test -v -run=ExampleWatcher
func ExampleWatcher() {
	// 假设在当前目录创建临时目录
	testDir := filepath.Join(".", "tmp-watcher-example")
	_ = os.MkdirAll(testDir, 0755)
	defer os.RemoveAll(testDir) // 演示结束后删除

	// 配置
	cfg := ConfigWatcher{
		WatchPaths:     []string{testDir},
		IgnorePatterns: []string{"*.tmp"},
		Debounce:       50 * time.Millisecond,
		WorkerCount:    4,
	}

	// 创建 Watcher
	w, err := NewWatcher(cfg)
	if err != nil {
		fmt.Println("Error creating watcher:", err)
		return
	}

	// 启动
	if err := w.Start(); err != nil {
		fmt.Println("Error starting watcher:", err)
		return
	}

	// 在 testDir 中创建一个文件
	filePath := filepath.Join(testDir, "example.txt")
	err = os.WriteFile(filePath, []byte("Hello watcher"), 0644)
	if err != nil {
		fmt.Println("Error creating file:", err)
	}

	// 等待一点时间，让事件有机会被捕获
	time.Sleep(100 * time.Millisecond)

	// 获取当前快照
	current := w.GetCurrentSnapshot()
	fmt.Printf("Current snapshot ID: <snapshot-id>\n")

	// 判断文件是否在快照里
	if meta, ok := current.Files[filePath]; ok {
		// 使用filepath.ToSlash确保路径分隔符统一
		relPath := filepath.ToSlash(meta.Path)
		if !strings.HasPrefix(relPath, "./") {
			relPath = "./" + relPath
		}
		fmt.Printf("File: %s, Size=%d, Hash=<hash-value>\n", relPath, meta.Size)
	}

	// 打印事件通道中的信息（若有）
selectLoop:
	for {
		select {
		case evt := <-w.EventChan:
			// 使用filepath.ToSlash确保路径分隔符统一
			relPath := filepath.ToSlash(evt.FilePath)
			if !strings.HasPrefix(relPath, "./") {
				relPath = "./" + relPath
			}
			// 简化事件类型，只显示主要操作
			op := evt.Op
			if op&fsnotify.Create == fsnotify.Create {
				op = fsnotify.Create
			} else if op&fsnotify.Write == fsnotify.Write {
				op = fsnotify.Write
			}
			fmt.Printf("Event: %s %s\n", op.String(), relPath)
		default:
			break selectLoop
		}
	}

	// 停止监控
	w.Stop()

	// Output:
	// Current snapshot ID: <snapshot-id>
	// File: ./tmp-watcher-example/example.txt, Size=13, Hash=<hash-value>
	// Event: CREATE ./tmp-watcher-example/example.txt
}
