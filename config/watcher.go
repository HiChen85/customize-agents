package config

import (
	"log/slog"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

type ReloadCallback func(oldCfg, newCfg *Config)

type ConfigWatcher struct {
	path      string
	watcher   *fsnotify.Watcher
	callbacks []ReloadCallback
	stopCh    chan struct{}
	debounce  time.Duration
	mu        sync.Mutex
	lastCfg   *Config
}

func NewConfigWatcher(path string, debounce time.Duration) (*ConfigWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if debounce <= 0 {
		debounce = 1 * time.Second
	}

	cfg, _ := Load(path)

	return &ConfigWatcher{
		path:     path,
		watcher:  w,
		stopCh:   make(chan struct{}),
		debounce: debounce,
		lastCfg:  cfg,
	}, nil
}

func (cw *ConfigWatcher) OnReload(cb ReloadCallback) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	cw.callbacks = append(cw.callbacks, cb)
}

func (cw *ConfigWatcher) Start() error {
	if err := cw.watcher.Add(cw.path); err != nil {
		return err
	}
	go cw.watchLoop()
	return nil
}

func (cw *ConfigWatcher) Stop() error {
	select {
	case <-cw.stopCh:
	default:
		close(cw.stopCh)
	}
	return cw.watcher.Close()
}

func (cw *ConfigWatcher) watchLoop() {
	var timer *time.Timer

	for {
		select {
		case <-cw.stopCh:
			if timer != nil {
				timer.Stop()
			}
			return

		case event, ok := <-cw.watcher.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				if timer != nil {
					timer.Stop()
				}
				timer = time.AfterFunc(cw.debounce, func() {
					cw.reload()
				})
			}

		case err, ok := <-cw.watcher.Errors:
			if !ok {
				return
			}
			slog.Warn("config watcher error", "error", err)
		}
	}
}

func (cw *ConfigWatcher) reload() {
	newCfg, err := Load(cw.path)
	if err != nil {
		slog.Warn("config reload failed, keeping current config", "error", err)
		return
	}

	cw.mu.Lock()
	oldCfg := cw.lastCfg
	cw.lastCfg = newCfg
	callbacks := make([]ReloadCallback, len(cw.callbacks))
	copy(callbacks, cw.callbacks)
	cw.mu.Unlock()

	for _, cb := range callbacks {
		cb(oldCfg, newCfg)
	}

	slog.Info("config reloaded successfully")
}
