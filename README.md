# Watcher

English | [简体中文](README.zh-CN.md)

[![Go Reference](https://pkg.go.dev/badge/github.com/shuakami/watcher.svg)](https://pkg.go.dev/github.com/shuakami/watcher)
[![Go Report Card](https://goreportcard.com/badge/github.com/shuakami/watcher)](https://goreportcard.com/report/github.com/shuakami/watcher)
[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](https://www.gnu.org/licenses/gpl-3.0)
[![Release](https://img.shields.io/github/v/release/shuakami/watcher.svg)](https://github.com/shuakami/watcher/releases)

`Watcher` is a Go package for monitoring filesystem changes and generating versioned snapshots.

## ✨ Features

- **📂 Recursive Monitoring**: Automatically capture create, modify, and delete events for files and directories
- **🌊 Event Debouncing**: Reduce event storms through debouncing
- **⚡ Concurrent Processing**: Process file changes concurrently using worker pools
- **📸 Snapshot Management**: Generate new snapshots for each change and maintain a DAG of snapshots
- **📝 File Metadata**: Record file size, modification time, hash, and other metadata for each snapshot
- **🔔 Event Notification**: Expose file change events through event channels
- **🔒 Thread Safety**: Ensure concurrent access safety using sync.RWMutex

## 📦 Installation

```bash
go get github.com/shuakami/watcher
```

## 🚀 Quick Start

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

	// Handle events
	go func() {
		for evt := range w.EventChan {
			fmt.Printf("Event: %s %s\n", evt.Op.String(), evt.FilePath)
		}
	}()

	// Run for a while then stop
	time.Sleep(10 * time.Second)
	w.Stop()
}
```

## 📚 Documentation

For detailed documentation, please visit [GoDoc](https://pkg.go.dev/github.com/shuakami/watcher)

## 🤝 Contributing

Issues and Pull Requests are welcome!

## 📄 License

This project is licensed under the [GNU General Public License v3.0](LICENSE).