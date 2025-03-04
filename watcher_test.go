package watcher

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestHashFile 测试hashFile函数
func TestHashFile(t *testing.T) {
	tmpFile, err := ioutil.TempFile("", "testfile-")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	_, _ = tmpFile.WriteString("Hello World!")
	_ = tmpFile.Sync()

	hashVal, err := hashFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("hashFile failed: %v", err)
	}
	if hashVal == "" {
		t.Error("hashFile returned empty string")
	}
}

// TestIsIgnored 测试 isIgnored 函数
func TestIsIgnored(t *testing.T) {
	w := Watcher{
		cfg: ConfigWatcher{
			IgnorePatterns: []string{"*.tmp", ".git"},
		},
	}

	cases := []struct {
		path   string
		ignore bool
	}{
		{"file.tmp", true},
		{"file.log", false},
		{"main.git", false},
		{".git", true},
		{"something/.git", false}, // 因为是base匹配，所以不忽略
	}

	for _, c := range cases {
		got := w.isIgnored(c.path)
		if got != c.ignore {
			t.Errorf("isIgnored(%s) = %v; want %v", c.path, got, c.ignore)
		}
	}
}

// TestWatcherBasic 测试 Watcher 的基本功能
func TestWatcherBasic(t *testing.T) {
	testDir, err := ioutil.TempDir("", "watcher-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(testDir)

	cfg := ConfigWatcher{
		WatchPaths: []string{testDir},
		Debounce:   5 * time.Millisecond,
	}
	w, err := NewWatcher(cfg)
	if err != nil {
		t.Fatalf("NewWatcher failed: %v", err)
	}
	defer w.Stop()

	if err := w.Start(); err != nil {
		t.Fatalf("Watcher Start failed: %v", err)
	}

	// 创建一个测试文件
	filePath := filepath.Join(testDir, "test.txt")
	err = ioutil.WriteFile(filePath, []byte("hello"), 0644)
	if err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// 等待事件传递
	time.Sleep(50 * time.Millisecond)

	current := w.GetCurrentSnapshot()
	if current == nil {
		t.Fatal("current snapshot is nil")
	}

	metadata, ok := current.Files[filePath]
	if !ok {
		t.Fatalf("file metadata not found in current snapshot")
	}
	if metadata.Hash == "" {
		t.Errorf("expected non-empty hash for file")
	}

	// 验证 EventChan 是否收到事件
	select {
	case evt := <-w.EventChan:
		if evt.FilePath != filePath {
			t.Errorf("expected event for %s, got %s", filePath, evt.FilePath)
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for file event")
	}

	// 再删除该文件
	err = os.Remove(filePath)
	if err != nil {
		t.Fatalf("failed to remove test file: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	current = w.GetCurrentSnapshot()
	if _, ok := current.Files[filePath]; ok {
		t.Errorf("file metadata should be removed after deletion")
	}
}

// BenchmarkHashFile 基准测试
func BenchmarkHashFile(b *testing.B) {
	tmpFile, _ := ioutil.TempFile("", "benchfile-")
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	data := make([]byte, 1024*1024) // 1MB
	tmpFile.Write(data)
	tmpFile.Sync()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = hashFile(tmpFile.Name())
	}
}
