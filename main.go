package main

import (
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	photosDir  = envOr("PHOTOS_DIR", "/photos")
	stateFile  = envOr("STATE_FILE", "/state/shuffle.json")
	listenAddr = envOr("LISTEN_ADDR", ":8080")
)

var supportedExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".avif": true,
}

// activeSubfolder returns the photo subfolder that should be shown today.
func activeSubfolder() string {
	now := time.Now()
	m, d := int(now.Month()), now.Day()
	switch {
	case m == 12:
		return "december"
	case m == 5 && d == 9:
		return "ivy"
	case m == 6 && d == 27:
		return "jason"
	case m == 11 && d == 13:
		return "kirsty"
	case m == 2 && d == 19:
		return "lan"
	default:
		return "default"
	}
}

type shuffleState struct {
	Queue     []string `json:"queue"`
	Index     int      `json:"index"`
	Subfolder string   `json:"subfolder"` // which subfolder this queue was built from
}

type server struct {
	mu    sync.Mutex
	state shuffleState
}

func main() {
	s := &server{}
	s.load()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("GET /photo", s.handlePhoto)

	log.Printf("listening on %s  photos=%s  state=%s", listenAddr, photosDir, stateFile)
	log.Fatal(http.ListenAndServe(listenAddr, mux))
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(indexHTML))
}

func (s *server) handlePhoto(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.advance()
	path := s.currentPath()
	s.mu.Unlock()

	if path == "" {
		http.Error(w, "no photos found", http.StatusNotFound)
		return
	}
	http.ServeFile(w, r, path)
}

func (s *server) currentPath() string {
	if len(s.state.Queue) == 0 {
		return ""
	}
	idx := s.state.Index
	if idx < 0 || idx >= len(s.state.Queue) {
		idx = 0
	}
	return filepath.Join(photosDir, s.state.Subfolder, s.state.Queue[idx])
}

func (s *server) advance() {
	// If the date has moved us into a different subfolder, start fresh.
	if activeSubfolder() != s.state.Subfolder {
		log.Printf("subfolder changed %q → %q, rebuilding", s.state.Subfolder, activeSubfolder())
		s.rebuild()
		s.save()
		return
	}

	if len(s.state.Queue) == 0 {
		s.rebuild()
		return
	}
	s.state.Index++
	if s.state.Index >= len(s.state.Queue) {
		s.rebuild()
	}
	s.save()
}

func (s *server) rebuild() {
	sub := activeSubfolder()
	dir := filepath.Join(photosDir, sub)

	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("rebuild: cannot read %s: %v", dir, err)
		return
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if supportedExts[ext] {
			names = append(names, e.Name())
		}
	}

	if len(names) == 0 {
		log.Printf("rebuild: no photos found in %s", dir)
		s.state = shuffleState{Subfolder: sub}
		return
	}

	rand.Shuffle(len(names), func(i, j int) { names[i], names[j] = names[j], names[i] })

	// Avoid repeating the last photo shown when reshuffling within the same subfolder.
	if s.state.Subfolder == sub && len(s.state.Queue) > 0 && len(names) > 1 {
		last := s.state.Queue[len(s.state.Queue)-1]
		if names[0] == last {
			names[0], names[1] = names[1], names[0]
		}
	}

	s.state = shuffleState{Queue: names, Index: 0, Subfolder: sub}
	log.Printf("rebuild: shuffled %d photos from %s", len(names), sub)
}

func (s *server) load() {
	data, err := os.ReadFile(stateFile)
	if err != nil {
		log.Printf("load: starting fresh (%v)", err)
		s.rebuild()
		s.save()
		return
	}
	if err := json.Unmarshal(data, &s.state); err != nil {
		log.Printf("load: corrupt state, rebuilding (%v)", err)
		s.rebuild()
		s.save()
		return
	}
	// Switch subfolder if date has changed since last run.
	if s.state.Subfolder != activeSubfolder() {
		log.Printf("load: subfolder changed %q → %q", s.state.Subfolder, activeSubfolder())
		s.rebuild()
		s.save()
		return
	}
	if !s.validateQueue() {
		log.Printf("load: photo set changed, rebuilding")
		s.rebuild()
		s.save()
	}
}

func (s *server) validateQueue() bool {
	if len(s.state.Queue) == 0 {
		return false
	}
	dir := filepath.Join(photosDir, s.state.Subfolder)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	disk := make(map[string]bool, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if supportedExts[ext] {
				disk[e.Name()] = true
			}
		}
	}
	if len(disk) != len(s.state.Queue) {
		return false
	}
	for _, name := range s.state.Queue {
		if !disk[name] {
			return false
		}
	}
	return true
}

func (s *server) save() {
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		log.Printf("save: marshal error: %v", err)
		return
	}
	if err := os.WriteFile(stateFile, data, 0644); err != nil {
		log.Printf("save: write error: %v", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

const indexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Photos</title>
<style>
  *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
  html, body { width: 100%; height: 100%; background: #000; overflow: hidden; }
  .stage { position: relative; width: 100%; height: 100%; }
  .slide {
    position: absolute; inset: 0;
    display: flex; align-items: center; justify-content: center;
    opacity: 0; transition: opacity 1.5s ease-in-out;
  }
  .slide.active { opacity: 1; }
  .slide img {
    width: 100%; height: 100%;
    object-fit: contain;
  }
</style>
</head>
<body>
<div class="stage">
  <div class="slide active" id="a"><img id="img-a" src="/photo" alt=""></div>
  <div class="slide"        id="b"><img id="img-b" src=""       alt=""></div>
</div>
<script>
  const INTERVAL_MS = 60 * 1000;
  let front = 'a';

  async function advance() {
    const back = front === 'a' ? 'b' : 'a';
    const backImg = document.getElementById('img-' + back);
    try {
      await new Promise((resolve, reject) => {
        backImg.onload  = resolve;
        backImg.onerror = reject;
        backImg.src = '/photo?t=' + Date.now();
      });
      document.getElementById(back).classList.add('active');
      document.getElementById(front).classList.remove('active');
      front = back;
    } catch (e) {
      console.error('advance error', e);
    }
  }

  setInterval(advance, INTERVAL_MS);
</script>
</body>
</html>
`
