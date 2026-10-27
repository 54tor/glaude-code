package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ideEvent : type create|write|remove|rename pour un changement direct, ou resync (path vide) quand des évènements
// ont été perdus (tampon plein, débordement d'inotify, passage au sondage) : le client relit alors ce qu'il affiche.
type ideEvent struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

var (
	ideDebounce     = 150 * time.Millisecond
	idePollInterval = 3 * time.Second
	ideIdleTTL      = 2 * time.Minute
	idePing         = 25 * time.Second
	ideNewWatcher   = fsnotify.NewWatcher
	ideWatchAdd     = func(fw *fsnotify.Watcher, p string) error { return fw.Add(p) }
	ideSubBuf       = 256
	ideTreeBudget   = time.Second
)

var ideResync = ideEvent{Type: "resync"}

const (
	ideWatchMax = 2000
	ideSubPaths = 200
)

type ideSig struct {
	mtime, size int64
	dir         bool
}

type ideSub struct {
	dirs map[string]bool
	ch   chan ideEvent
}

type ideWorkspace struct {
	dir  string
	root *os.Root
	done chan struct{}

	mu       sync.Mutex
	closed   bool
	ix       *ideIdx
	gen      int
	failed   bool
	subs     map[*ideSub]bool
	watcher  *fsnotify.Watcher
	idxDirs  map[string]bool
	declared map[string]int
	poll     bool
	snaps    map[string]map[string]ideSig
	last     time.Time
	timer    *time.Timer
}

var (
	ideWSMu       sync.Mutex
	ideWorkspaces = map[string]*ideWorkspace{}
)

// ideWS ne prend jamais w.mu sous ideWSMu : un lot long sur un espace bloquerait l'ouverture de tous les autres.
func ideWS(dir string) (*ideWorkspace, error) {
	ideWSMu.Lock()
	if w := ideWorkspaces[dir]; w != nil {
		ideWSMu.Unlock()
		w.mu.Lock()
		closed := w.closed
		if !closed {
			w.touchLocked()
		}
		w.mu.Unlock()
		if closed { // libéré entre-temps : expire et ideCloseAll l'ont déjà retiré de la table
			return ideWS(dir)
		}
		return w, nil
	}
	defer ideWSMu.Unlock()
	root, err := ideOpenRoot(dir)
	if err != nil {
		return nil, err
	}
	w := &ideWorkspace{dir: dir, root: root, done: make(chan struct{}), subs: map[*ideSub]bool{},
		idxDirs: map[string]bool{}, declared: map[string]int{}, snaps: map[string]map[string]ideSig{}}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.timer = time.AfterFunc(ideIdleTTL, w.expire)
	w.touchLocked()
	if fw, err := ideNewWatcher(); err != nil {
		w.fallbackLocked()
	} else {
		w.watcher = fw
		go w.loop(fw, ideDebounce)
	}
	ideWorkspaces[dir] = w
	return w, nil
}

// ideCloseAll ferme tous les espaces (arrêt du serveur, fin de test) : leurs flux /fs-watch se terminent.
func ideCloseAll() {
	ideWSMu.Lock()
	defer ideWSMu.Unlock()
	for d, w := range ideWorkspaces {
		w.mu.Lock()
		w.closeLocked()
		w.mu.Unlock()
		delete(ideWorkspaces, d)
	}
}

func (w *ideWorkspace) touchLocked() {
	w.last = time.Now()
	if w.timer != nil {
		w.timer.Reset(ideIdleTTL)
	}
}

func (w *ideWorkspace) expire() {
	ideWSMu.Lock()
	defer ideWSMu.Unlock()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || len(w.subs) > 0 {
		return
	}
	if d := ideIdleTTL - time.Since(w.last); d > 0 {
		w.timer.Reset(d)
		return
	}
	w.closeLocked()
	if ideWorkspaces[w.dir] == w {
		delete(ideWorkspaces, w.dir)
	}
}

func (w *ideWorkspace) closeLocked() {
	if w.closed {
		return
	}
	w.closed = true
	w.timer.Stop()
	if w.watcher != nil {
		w.watcher.Close()
		w.watcher = nil
	}
	w.root.Close()
	close(w.done)
	// Plus rien n'écrit : flushLocked et fallbackLocked s'arrêtent sur w.closed.
	for s := range w.subs {
		close(s.ch)
	}
	clear(w.subs)
}

// reopen sert l'appelant qui a obtenu l'espace juste avant sa libération.
func (w *ideWorkspace) reopen() *ideWorkspace {
	nw, err := ideWS(w.dir)
	if err != nil {
		return nil
	}
	return nw
}

func (w *ideWorkspace) Index() ([]ideIndexEntry, string) {
	ix := w.idx()
	return ix.entries, ix.state
}

// idx lance la construction unique au premier appel, puis une nouvelle après un échec ; ne bloque jamais.
func (w *ideWorkspace) idx() ideIdx {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		if nw := w.reopen(); nw != nil {
			return nw.idx()
		}
		return ideIdx{state: "partial"}
	}
	defer w.mu.Unlock()
	w.touchLocked()
	cur := w.ix
	if cur != nil && !w.failed {
		return *cur
	}
	if cur == nil {
		w.ix = &ideIdx{state: "building"}
		cur = w.ix
	}
	w.failed = false
	go w.build(w.gen)
	return *cur
}

// build ne publie que pour sa génération : une invalidation survenue pendant le parcours l'emporte.
func (w *ideWorkspace) build(gen int) {
	defer func() {
		if recover() != nil {
			w.mu.Lock()
			if w.gen == gen {
				w.ix, w.failed = &ideIdx{state: "partial", set: map[string]bool{}}, true
			}
			w.mu.Unlock()
		}
	}()
	ideBuildHook()
	entries, dirs, partial := ideWalk(w.root, ideIndexLimits)
	set := make(map[string]bool, len(entries))
	for _, e := range entries {
		set[e.Path] = true
	}
	sort.SliceStable(dirs, func(i, j int) bool { return strings.Count(dirs[i], "/") < strings.Count(dirs[j], "/") })
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.gen != gen {
		return
	}
	// Au sondage, l'index n'est plus tenu à jour hors des dossiers déclarés.
	st := "ready"
	if partial || w.poll {
		st = "partial"
	}
	w.ix = &ideIdx{state: st, entries: entries, set: set}
	for _, d := range dirs {
		if len(w.idxDirs) >= ideWatchMax {
			break
		}
		if w.watchLocked(d) {
			w.idxDirs[d] = true
		}
	}
}

// watchLocked n'ajoute que des dossiers qui viennent de passer ideLstat par la racine : fsnotify ouvre par
// chemin absolu, donc un remplacement par un lien entre ce contrôle et l'ajout reste possible.
func (w *ideWorkspace) watchLocked(rel string) bool {
	if w.watcher == nil || w.closed {
		return false
	}
	if w.idxDirs[rel] || w.declared[rel] > 0 {
		return true
	}
	fi, err := ideLstat(w.root, rel)
	if err != nil || !fi.IsDir() {
		return false
	}
	if err := ideWatchAdd(w.watcher, filepath.Join(w.dir, filepath.FromSlash(rel))); err != nil {
		// Seules les limites d'inotify condamnent la veille ; un dossier refusé (EACCES…) est seulement sauté.
		if errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EMFILE) || errors.Is(err, syscall.ENFILE) {
			w.fallbackLocked()
		}
		return false
	}
	return true
}

func (w *ideWorkspace) unwatchLocked(rel string) {
	if w.watcher != nil && !w.idxDirs[rel] && w.declared[rel] == 0 {
		w.watcher.Remove(filepath.Join(w.dir, filepath.FromSlash(rel)))
	}
}

// lostLocked : des changements ont pu échapper ; les abonnés relisent et le prochain /fs-index reconstruit.
func (w *ideWorkspace) lostLocked() {
	w.ix = nil
	w.gen++
	for s := range w.subs {
		s.send(ideResync)
	}
}

// send vide un tampon plein au profit d'un seul resync, qui rend caducs les évènements en attente ; ce resync
// n'est jamais perdu et plusieurs débordements n'en laissent qu'un. Seul flushLocked (sous w.mu) écrit.
func (s *ideSub) send(e ideEvent) {
	select {
	case s.ch <- e:
		return
	default:
	}
	for {
		select {
		case <-s.ch:
			continue
		default:
		}
		break
	}
	s.ch <- ideResync
}

// fallbackLocked : sans inotify (épuisé, débordé, indisponible), seuls les dossiers déclarés sont sondés.
func (w *ideWorkspace) fallbackLocked() {
	if w.poll || w.closed {
		return
	}
	w.poll = true
	if w.watcher != nil {
		w.watcher.Close()
		w.watcher = nil
	}
	for d := range w.declared {
		w.snaps[d] = w.scanLocked(d)
	}
	w.lostLocked()
	t := time.NewTicker(idePollInterval)
	go func() {
		defer t.Stop()
		for {
			select {
			case <-w.done:
				return
			case <-t.C:
				w.pollOnce()
			}
		}
	}()
}

func (w *ideWorkspace) scanLocked(rel string) map[string]ideSig {
	out := map[string]ideSig{}
	if fi, err := ideLstat(w.root, rel); err != nil || !fi.IsDir() {
		return out
	}
	d, err := w.root.Open(rel)
	if err != nil {
		return out
	}
	des, _ := d.ReadDir(-1)
	d.Close()
	for _, de := range des {
		s := ideSig{dir: de.IsDir()}
		if info, err := de.Info(); err == nil {
			s.mtime, s.size = info.ModTime().UnixNano(), info.Size()
		}
		out[de.Name()] = s
	}
	return out
}

func ideJoin(dir, name string) string {
	if dir == "." {
		return name
	}
	if name == "." {
		return dir
	}
	return dir + "/" + name
}

func (w *ideWorkspace) pollOnce() {
	w.mu.Lock()
	defer w.mu.Unlock()
	var evs []ideEvent
	for d := range w.declared {
		prev, cur := w.snaps[d], w.scanLocked(d)
		for n, s := range cur {
			if p, ok := prev[n]; !ok {
				evs = append(evs, ideEvent{ideJoin(d, n), "create"})
			} else if p != s {
				evs = append(evs, ideEvent{ideJoin(d, n), "write"})
			}
		}
		for n := range prev {
			if _, ok := cur[n]; !ok {
				evs = append(evs, ideEvent{ideJoin(d, n), "remove"})
			}
		}
		w.snaps[d] = cur
	}
	sort.Slice(evs, func(i, j int) bool { return evs[i].Path < evs[j].Path })
	w.flushLocked(evs)
}

func (w *ideWorkspace) loop(fw *fsnotify.Watcher, debounce time.Duration) {
	var pending []ideEvent
	seen := map[ideEvent]bool{}
	var tick <-chan time.Time
	for {
		select {
		case <-w.done:
			return
		case ev, ok := <-fw.Events:
			if !ok {
				w.mu.Lock()
				w.flushLocked(pending)
				w.mu.Unlock()
				return
			}
			e, keep := w.convert(ev)
			if !keep {
				continue
			}
			if !seen[e] {
				seen[e] = true
				pending = append(pending, e)
			}
			if tick == nil {
				tick = time.After(debounce)
			}
		case err, ok := <-fw.Errors:
			if !ok {
				return
			}
			if errors.Is(err, fsnotify.ErrEventOverflow) {
				w.mu.Lock()
				w.flushLocked(pending)
				w.fallbackLocked()
				w.mu.Unlock()
				pending, tick = nil, nil
				clear(seen)
			}
		case <-tick:
			w.mu.Lock()
			w.flushLocked(pending)
			w.mu.Unlock()
			pending, tick = nil, nil
			clear(seen)
		}
	}
}

func (w *ideWorkspace) convert(ev fsnotify.Event) (ideEvent, bool) {
	rel, err := filepath.Rel(w.dir, ev.Name)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ideEvent{}, false
	}
	e := ideEvent{Path: filepath.ToSlash(rel)}
	switch {
	case ev.Has(fsnotify.Create):
		e.Type = "create"
	case ev.Has(fsnotify.Write):
		e.Type = "write"
	case ev.Has(fsnotify.Remove):
		e.Type = "remove"
	case ev.Has(fsnotify.Rename):
		e.Type = "rename"
	default:
		return ideEvent{}, false
	}
	return e, true
}

func (w *ideWorkspace) flushLocked(evs []ideEvent) {
	if len(evs) == 0 || w.closed {
		return
	}
	w.applyLocked(evs)
	for _, e := range evs {
		if e.Type == "create" && w.declared[e.Path] > 0 && w.watcher != nil {
			// inotify a lâché le dossier quand il a été supprimé : sans nouvel Add, sa recréation (git checkout, rm puis mkdir) reste muette.
			ideWatchAdd(w.watcher, filepath.Join(w.dir, filepath.FromSlash(e.Path)))
		}
		parent := path.Dir(e.Path)
		for s := range w.subs {
			if s.dirs[parent] {
				s.send(e)
			}
		}
	}
}

func ideIndexable(rel string) bool {
	parts := strings.Split(rel, "/")
	for _, p := range parts {
		if ideIgnored(p) {
			return false
		}
	}
	return len(parts)-1 <= ideIndexLimits.Depth
}

// addTreeLocked surveille puis parcourt un dossier arrivé (mkdir suivi d'une écriture rapide, mv d'un dossier
// plein) : dans cet ordre, rien de ce qui y apparaît ensuite n'échappe.
// Le budget (une échéance, les entrées restantes) est commun à tous les dossiers d'un lot : une rafale de
// mkdir ne le multiplie pas. Renvoie false quand le parcours est incomplet.
func (w *ideWorkspace) addTreeLocked(rel string, byPath map[string]ideIndexEntry, added map[string]bool, deadline time.Time) bool {
	if len(w.idxDirs) < ideWatchMax && w.watchLocked(rel) {
		w.idxDirs[rel] = true
	}
	lim := ideLimits{ideIndexLimits.Entries - len(byPath), ideIndexLimits.Depth - strings.Count(rel, "/") - 1, time.Until(deadline)}
	if lim.Entries <= 0 || lim.Depth < 0 || lim.Time <= 0 {
		return false
	}
	sub, err := w.root.OpenRoot(rel)
	if err != nil {
		return true
	}
	defer sub.Close()
	es, dirs, partial := ideWalk(sub, lim)
	for _, e := range es {
		e.Path = rel + "/" + e.Path
		byPath[e.Path] = e
		added[e.Path] = true
	}
	for _, d := range dirs {
		if d = ideJoin(rel, d); d != rel && len(w.idxDirs) < ideWatchMax && w.watchLocked(d) {
			w.idxDirs[d] = true
		}
	}
	return !partial
}

// applyLocked met l'index à jour par copie, l'ancien instantané restant lisible par les appelants.
func (w *ideWorkspace) applyLocked(evs []ideEvent) {
	if w.ix == nil || w.ix.state == "building" {
		return
	}
	byPath := make(map[string]ideIndexEntry, len(w.ix.entries)+len(evs))
	for _, e := range w.ix.entries {
		byPath[e.Path] = e
	}
	st := w.ix.state
	old := w.ix.entries // trié : les descendants d'un dossier y forment une plage contiguë
	added := map[string]bool{}
	// ponytail: parcours sous verrou borné par ideTreeBudget et par lot ; un gros dossier déplacé fait attendre /fs-index d'autant
	deadline := time.Now().Add(ideTreeBudget)
	for _, e := range evs {
		if !ideIndexable(e.Path) {
			continue
		}
		fi, err := ideLstat(w.root, e.Path)
		switch {
		case err != nil || e.Type == "remove" || e.Type == "rename":
			delete(byPath, e.Path)
			delete(added, e.Path)
			pre := e.Path + "/"
			for i := sort.Search(len(old), func(i int) bool { return old[i].Path >= pre }); i < len(old) && strings.HasPrefix(old[i].Path, pre); i++ {
				delete(byPath, old[i].Path)
			}
			// Seul un dossier surveillé a pu recevoir des entrées pendant ce lot ou avoir des sous-dossiers surveillés.
			if w.idxDirs[e.Path] {
				for p := range added {
					if strings.HasPrefix(p, pre) {
						delete(byPath, p)
						delete(added, p)
					}
				}
				for d := range w.idxDirs {
					if d == e.Path || strings.HasPrefix(d, pre) {
						delete(w.idxDirs, d)
					}
				}
			}
		case fi.IsDir():
			if !w.addTreeLocked(e.Path, byPath, added, deadline) {
				st = "partial"
			}
		case fi.Mode().IsRegular():
			if _, ok := byPath[e.Path]; ok || len(byPath) < ideIndexLimits.Entries {
				byPath[e.Path] = ideIndexEntry{e.Path, fi.Size(), fi.ModTime().Unix()}
				added[e.Path] = true
			}
		}
	}
	entries := make([]ideIndexEntry, 0, len(byPath))
	set := make(map[string]bool, len(byPath))
	for p, e := range byPath {
		entries = append(entries, e)
		set[p] = true
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	w.ix = &ideIdx{state: st, entries: entries, set: set}
}

func (w *ideWorkspace) Subscribe(paths []string) (<-chan ideEvent, func()) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		if nw := w.reopen(); nw != nil {
			return nw.Subscribe(paths)
		}
		return make(chan ideEvent), func() {}
	}
	defer w.mu.Unlock()
	s := &ideSub{dirs: map[string]bool{}, ch: make(chan ideEvent, ideSubBuf)}
	for _, p := range paths {
		if len(s.dirs) >= ideSubPaths {
			break
		}
		rel, err := ideRel(p, true)
		if err != nil || s.dirs[rel] {
			continue
		}
		if fi, err := ideLstat(w.root, rel); err != nil || !fi.IsDir() {
			continue
		}
		if !w.poll && !w.watchLocked(rel) && !w.poll {
			continue
		}
		if w.poll && w.declared[rel] == 0 {
			w.snaps[rel] = w.scanLocked(rel)
		}
		s.dirs[rel] = true
		w.declared[rel]++
	}
	w.subs[s] = true
	var once sync.Once
	return s.ch, func() {
		once.Do(func() {
			w.mu.Lock()
			defer w.mu.Unlock()
			delete(w.subs, s)
			for d := range s.dirs {
				if w.declared[d]--; w.declared[d] <= 0 {
					delete(w.declared, d)
					delete(w.snaps, d)
					w.unwatchLocked(d)
				}
			}
			if !w.closed {
				w.touchLocked()
			}
		})
	}
}

func routesIDEWatch(mux *http.ServeMux) {
	mux.HandleFunc("GET /fs-watch", func(rw http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		dir, ok := ideKnownDir(q.Get("dir"))
		if !ok {
			ideFail(rw, errIdeDir)
			return
		}
		ws, err := ideWS(dir)
		if err != nil {
			ideFail(rw, err)
			return
		}
		ch, cancel := ws.Subscribe(q["paths"])
		defer cancel()
		rw.Header().Set("Content-Type", "text/event-stream")
		rw.Header().Set("Cache-Control", "no-store")
		rw.Header().Set("X-Accel-Buffering", "no")
		rw.WriteHeader(http.StatusOK)
		rc := http.NewResponseController(rw)
		rc.Flush()
		ping := time.NewTicker(idePing)
		defer ping.Stop()
		for {
			var out []byte
			select {
			case <-r.Context().Done():
				return
			case e, ok := <-ch:
				if !ok {
					return
				}
				b, _ := json.Marshal(e)
				out = append(append([]byte("data: "), b...), "\n\n"...)
			case <-ping.C:
				out = []byte(": ping\n\n")
			}
			if _, err := rw.Write(out); err != nil || rc.Flush() != nil {
				return
			}
		}
	})
}
