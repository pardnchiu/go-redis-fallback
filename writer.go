package redisFallback

import (
	"encoding/json"
	"os"
	"sync"
)

func (w *Writer) start() {
	go func() {
		for {
			select {
			case req := <-w.queue:
				w.mutex.Lock()
				w.pending[req.Key] = req.Data
				w.mutex.Unlock()
			case <-w.timer.C:
				w.write()
			}
		}
	}()
}

func (w *Writer) write() {
	w.mutex.Lock()
	// * nothing to write
	if len(w.pending) == 0 {
		w.mutex.Unlock()
		return
	}

	list := make(map[string]interface{})
	for k, v := range w.pending {
		list[k] = v
	}
	w.pending = make(map[string]interface{})
	w.mutex.Unlock()

	var wg sync.WaitGroup
	for key, data := range list {
		wg.Add(1)
		go func(k string, d interface{}) {
			defer wg.Done()
			if item, ok := d.(Cache); ok {
				w.writeToFile(k, item)
			}
		}(key, data)
	}
	wg.Wait()
}

func (w *Writer) writeToFile(key string, cache Cache) error {
	path := getPath(w.config, key)

	// * Create fallback db directory
	if err := os.MkdirAll(path.folderPath, 0755); err != nil {
		return w.logger.Error(err, "Failed to create folder")
	}

	data, err := json.Marshal(cache)
	if err != nil {
		return w.logger.Error(err, "Failed to parse")
	}

	return os.WriteFile(path.filepath, data, 0644)
}
