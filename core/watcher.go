package core

import (
	"log"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// FileWatcher handles file system events for cache invalidation
type FileWatcher struct {
	watcher   *fsnotify.Watcher
	callbacks map[string][]func()
	mu        sync.RWMutex
	done      chan struct{}
}

// NewFileWatcher creates a new file system watcher
func NewFileWatcher() (*FileWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	fw := &FileWatcher{
		watcher:   watcher,
		callbacks: make(map[string][]func()),
		done:      make(chan struct{}),
	}

	// Start event processing goroutine
	go fw.processEvents()

	return fw, nil
}

// WatchFile adds a file to be watched for changes
func (fw *FileWatcher) WatchFile(path string, callback func()) error {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	// Add callback
	fw.callbacks[path] = append(fw.callbacks[path], callback)

	// Add to watcher if not already watched
	if len(fw.callbacks[path]) == 1 {
		if err := fw.watcher.Add(path); err != nil {
			delete(fw.callbacks, path)
			return err
		}
	}

	return nil
}

// WatchDirectory adds a directory to be watched for changes
func (fw *FileWatcher) WatchDirectory(path string, callback func()) error {
	return fw.WatchFile(path, callback)
}

// processEvents processes file system events
func (fw *FileWatcher) processEvents() {
	for {
		select {
		case event, ok := <-fw.watcher.Events:
			if !ok {
				return
			}

			fw.mu.RLock()
			callbacks := fw.callbacks[event.Name]
			fw.mu.RUnlock()

			// Execute all callbacks for this path
			for _, callback := range callbacks {
				go callback() // Run in goroutine to avoid blocking
			}

		case err, ok := <-fw.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("FileWatcher error: %v", err)

		case <-fw.done:
			return
		}
	}
}

// Close shuts down the file watcher
func (fw *FileWatcher) Close() error {
	close(fw.done)
	return fw.watcher.Close()
}
