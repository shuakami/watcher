package watcher

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// SnapshotNode 表示某一次快照(版本)的节点，形成一个DAG
//
// ID 是此版本的唯一标识，如 "snap-1234567890"
// ParentIDs 表示它可能有多个父版本（支持多分支/合并）
// CreatedAt 表示创建时间
// Description 表示对于本次快照的描述
// Files 存储该快照下每个文件的元信息
type SnapshotNode struct {
	ID          string                   // 唯一ID (如 v1234567890)
	ParentIDs   []string                 // 父版本(可能不止一个, 支持合并/多分支场景)
	CreatedAt   time.Time                // 创建时间
	Description string                   // 描述(可为空)
	Files       map[string]*FileMetadata // 当前快照下的文件映射
}

// FileMetadata 表示单个文件在某个版本/快照中的信息
//
// Path：该文件的完整路径
// Size：文件大小（单位：字节）
// ModTime：文件上次修改时间
// Hash：文件内容哈希(使用SHA-256)
// IsDirectory：是否为目录
// CreatedAt：记录此FileMetadata的时间
// LastModified：文件本身的修改时间（和ModTime含义相同，但保留是为了可扩展性）
type FileMetadata struct {
	Path         string    // 完整路径
	Size         int64     // 文件大小
	ModTime      time.Time // 修改时间
	Hash         string    // 文件内容哈希(如 SHA-256)
	IsDirectory  bool      // 是否目录
	CreatedAt    time.Time // 记录此条目时
	LastModified time.Time // 文件本身的修改时间
}

// ConfigWatcher 用于配置 Watcher
//
// WatchPaths：需要监控的路径（可指定多个）
// IgnorePatterns：需要忽略的文件(或目录)通配符，如 "*.tmp" 或 ".git"
// Debounce：事件合并的时间间隔, 默认 10ms
// WorkerCount：并发处理文件变更的最大worker数量, 默认 32
type ConfigWatcher struct {
	WatchPaths     []string      // 要监控的路径
	IgnorePatterns []string      // 要忽略的文件通配符
	Debounce       time.Duration // 事件合并的时间间隔, 默认 10ms
	WorkerCount    int           // 并发处理 Worker 数, 默认 32
}

// Watcher 负责监控文件系统变化 + 快照管理
//
// mu：对snapshots与current字段的读写上锁
// fsWatcher：底层使用github.com/fsnotify/fsnotify进行文件系统事件捕捉
// stopChan：用于停止所有后台goroutine
// snapshots：版本ID -> *SnapshotNode 的映射，维护了所有快照
// current：当前活跃版本(HEAD)
// aggChan, aggMap, aggMu, aggTicker：用于事件合并（Debounce）
// workerPool：并发处理文件变更的令牌池
// EventChan：向外部暴露的"文件变更事件"通道
type Watcher struct {
	mu        sync.RWMutex
	cfg       ConfigWatcher
	fsWatcher *fsnotify.Watcher

	stopChan chan struct{}

	snapshots map[string]*SnapshotNode
	current   *SnapshotNode

	// 事件合并(防抖)
	aggChan   chan fsnotify.Event
	aggMap    map[string]fsnotify.Op
	aggMu     sync.Mutex
	aggTicker *time.Ticker

	// 事件处理并发控制
	workerPool chan struct{}

	// 向外部暴露的事件通道
	EventChan chan FileEvent
}

// FileEvent 表示可供外部使用的"文件变更事件"结构
//
// FilePath：变更文件的路径
// Op：操作类型（fsnotify.Create / fsnotify.Write / fsnotify.Remove / fsnotify.Rename 等）
// NewSnap：触发此变更后产生的新快照（包含了全部文件信息的最新状态）
type FileEvent struct {
	FilePath string
	Op       fsnotify.Op
	NewSnap  *SnapshotNode
}

// NewWatcher 根据给定配置创建一个新的 Watcher
//
// 若 cfg.Debounce <= 0，则默认使用 10ms
// 若 cfg.WorkerCount <= 0，则默认使用 32
func NewWatcher(cfg ConfigWatcher) (*Watcher, error) {
	if cfg.Debounce <= 0 {
		cfg.Debounce = 10 * time.Millisecond
	}
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = 32
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	w := &Watcher{
		cfg:       cfg,
		fsWatcher: fsw,
		stopChan:  make(chan struct{}),

		snapshots: make(map[string]*SnapshotNode),

		aggChan:   make(chan fsnotify.Event, 100000),
		aggMap:    make(map[string]fsnotify.Op),
		aggTicker: time.NewTicker(cfg.Debounce),

		workerPool: make(chan struct{}, cfg.WorkerCount),
		EventChan:  make(chan FileEvent, 20000),
	}

	// 创建初始快照(空)
	initial := &SnapshotNode{
		ID:          w.newSnapID(),
		CreatedAt:   time.Now(),
		Description: "Initial snapshot",
		Files:       make(map[string]*FileMetadata),
	}
	w.snapshots[initial.ID] = initial
	w.current = initial

	return w, nil
}

// Start 启动文件监控
//
// 会递归扫描 cfg.WatchPaths 中的所有目录，并将它们加到 fsnotify.Watcher 中
// 然后启动2个后台goroutine：
//  1. runAggregator()：负责事件合并
//  2. runFsNotify()：读取 fsnotify 事件并投递到合并队列
func (w *Watcher) Start() error {
	// 1) 递归添加监控目录
	for _, path := range w.cfg.WatchPaths {
		err := filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() && !w.isIgnored(p) {
				e := w.fsWatcher.Add(p)
				if e != nil {
					fmt.Printf("Warning: cannot watch dir %s: %v\n", p, e)
				}
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("failed to walk watch path %s: %w", path, err)
		}
	}

	// 2) 启动事件合并goroutine
	go w.runAggregator()

	// 3) 启动 fsnotify 事件读取goroutine
	go w.runFsNotify()

	return nil
}

// Stop 停止监控
//
// 关闭 stopChan，停止所有goroutine，关闭底层 fsnotify.Watcher，停止ticker
// 在退出前flush一次合并队列中的事件，并最后关闭 EventChan
func (w *Watcher) Stop() {
	close(w.stopChan)
	_ = w.fsWatcher.Close()
	w.aggTicker.Stop()
	// 退出前 flush 一次
	w.flushAgg(true)
	close(w.EventChan)
}

// GetCurrentSnapshot 返回当前(最新)快照
//
// 并发安全
func (w *Watcher) GetCurrentSnapshot() *SnapshotNode {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.current
}

// GetSnapshotByID 根据快照ID获取快照
//
// 若找不到则返回nil
// 并发安全
func (w *Watcher) GetSnapshotByID(id string) *SnapshotNode {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.snapshots[id]
}

// ListAllSnapshots 列出所有已知快照
//
// 并发安全
func (w *Watcher) ListAllSnapshots() []*SnapshotNode {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]*SnapshotNode, 0, len(w.snapshots))
	for _, sn := range w.snapshots {
		out = append(out, sn)
	}
	return out
}

// runFsNotify 不断读取 fsnotify 的事件并投递到合并队列
func (w *Watcher) runFsNotify() {
	for {
		select {
		case ev := <-w.fsWatcher.Events:
			if w.isIgnored(ev.Name) {
				continue
			}
			// 如果是新建目录，需要额外Add
			if ev.Op&fsnotify.Create == fsnotify.Create {
				if fi, e2 := os.Stat(ev.Name); e2 == nil && fi.IsDir() {
					_ = w.fsWatcher.Add(ev.Name)
				}
			}
			w.queueAgg(ev)

		case err := <-w.fsWatcher.Errors:
			fmt.Printf("fsnotify error: %v\n", err)

		case <-w.stopChan:
			return
		}
	}
}

// runAggregator 负责对短时间内的事件进行合并
func (w *Watcher) runAggregator() {
	for {
		select {
		case ev := <-w.aggChan:
			w.aggMu.Lock()
			op, ok := w.aggMap[ev.Name]
			if !ok {
				w.aggMap[ev.Name] = ev.Op
			} else {
				w.aggMap[ev.Name] = op | ev.Op
			}
			w.aggMu.Unlock()

		case <-w.aggTicker.C:
			w.flushAgg(false)

		case <-w.stopChan:
			return
		}
	}
}

// flushAgg 将合并map(aggMap)中的事件批量提交给workerPool处理
// force=false时是周期性flush；force=true时是Stop()阶段最后一次flush
func (w *Watcher) flushAgg(force bool) {
	w.aggMu.Lock()
	tmp := make(map[string]fsnotify.Op, len(w.aggMap))
	for k, v := range w.aggMap {
		tmp[k] = v
	}
	w.aggMap = make(map[string]fsnotify.Op)
	w.aggMu.Unlock()

	for p, op := range tmp {
		select {
		case w.workerPool <- struct{}{}:
			// 提交给worker处理
			go func(fp string, fop fsnotify.Op) {
				defer func() { <-w.workerPool }()
				w.handleFileChange(fp, fop)
			}(p, op)
		default:
			// 如果workerPool已满，可以根据需要阻塞提交或者丢弃
			w.workerPool <- struct{}{}
			go func(fp string, fop fsnotify.Op) {
				defer func() { <-w.workerPool }()
				w.handleFileChange(fp, fop)
			}(p, op)
		}
	}
}

// queueAgg 将事件放入合并通道，若满则阻塞
func (w *Watcher) queueAgg(ev fsnotify.Event) {
	w.aggChan <- ev
}

// handleFileChange 进行"更新快照"的逻辑处理
// 当文件被创建/修改/删除时，都会创建一个新的快照(引用父快照的数据)，并在新快照的 Files 中更新对应文件
func (w *Watcher) handleFileChange(path string, op fsnotify.Op) {
	fileInfo, statErr := os.Stat(path)
	if statErr != nil && !os.IsNotExist(statErr) {
		fmt.Printf("Error stating file: %v\n", statErr)
		return
	}

	// 复制 currentSnap => newSnap
	w.mu.Lock()
	parentSnap := w.current
	newSnap := &SnapshotNode{
		ID:          w.newSnapID(),
		ParentIDs:   []string{parentSnap.ID},
		CreatedAt:   time.Now(),
		Description: fmt.Sprintf("Snapshot after %s on %s", op.String(), path),
		Files:       make(map[string]*FileMetadata),
	}
	// 复制父快照的所有文件信息
	for k, v := range parentSnap.Files {
		copyMeta := *v
		newSnap.Files[k] = &copyMeta
	}
	w.snapshots[newSnap.ID] = newSnap
	w.current = newSnap
	w.mu.Unlock()

	// 文件已删除 => 从 newSnap.Files 移除
	if os.IsNotExist(statErr) && (op&fsnotify.Remove == fsnotify.Remove) {
		w.mu.Lock()
		delete(newSnap.Files, path)
		w.mu.Unlock()

		w.emitFileEvent(path, op, newSnap)
		return
	}

	// 若是目录则不计算哈希
	isDir := false
	hashVal := ""
	if fileInfo != nil {
		isDir = fileInfo.IsDir()
		if !isDir {
			h, err := hashFile(path)
			if err != nil {
				fmt.Printf("Error hashing file %s: %v\n", path, err)
				// 这里return还是继续更新均可，但hash失败可能只是临时问题（（
				// 这里只打印错误，但仍继续更新
			} else {
				hashVal = h
			}
		}
	}

	// 更新 newSnap 中的该文件信息
	if fileInfo != nil {
		meta := &FileMetadata{
			Path:         path,
			Size:         fileInfo.Size(),
			ModTime:      fileInfo.ModTime(),
			Hash:         hashVal,
			IsDirectory:  isDir,
			CreatedAt:    time.Now(),
			LastModified: fileInfo.ModTime(),
		}

		w.mu.Lock()
		newSnap.Files[path] = meta
		w.mu.Unlock()
	}

	w.emitFileEvent(path, op, newSnap)
}

// emitFileEvent 向外部发送事件，若通道满则阻塞
func (w *Watcher) emitFileEvent(path string, op fsnotify.Op, snap *SnapshotNode) {
	w.EventChan <- FileEvent{FilePath: path, Op: op, NewSnap: snap}
}

// isIgnored 判断路径是否匹配 cfg.IgnorePatterns
func (w *Watcher) isIgnored(path string) bool {
	base := filepath.Base(path)
	for _, pat := range w.cfg.IgnorePatterns {
		matched, _ := filepath.Match(pat, base)
		if matched {
			// 如果是在子目录中，且模式不包含路径分隔符，则不忽略
			if filepath.Dir(path) != "." && !strings.Contains(pat, string(os.PathSeparator)) {
				return false
			}
			return true
		}
	}
	return false
}

// hashFile 计算文件的SHA-256哈希值
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	_, err = io.Copy(h, f)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// newSnapID 生成新快照ID，使用纳秒时间戳
func (w *Watcher) newSnapID() string {
	return fmt.Sprintf("snap-%d", time.Now().UnixNano())
}
