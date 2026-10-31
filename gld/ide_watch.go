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
	ideWatchAdd     = func(watcher *fsnotify.Watcher, dirPath string) error {
		return watcher.Add(dirPath)
	}
	ideSubBuf     = 256
	ideTreeBudget = time.Second
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
	if existing := ideWorkspaces[dir]; existing != nil {
		ideWSMu.Unlock()
		existing.mu.Lock()
		closed := existing.closed
		if !closed {
			existing.touchLocked()
		}
		existing.mu.Unlock()
		if closed { // libéré entre-temps : expire et ideCloseAll l'ont déjà retiré de la table
			return ideWS(dir)
		}
		return existing, nil
	}
	defer ideWSMu.Unlock()
	root, err := ideOpenRoot(dir)
	if err != nil {
		return nil, err
	}
	workspace := &ideWorkspace{
		dir:      dir,
		root:     root,
		done:     make(chan struct{}),
		subs:     map[*ideSub]bool{},
		idxDirs:  map[string]bool{},
		declared: map[string]int{},
		snaps:    map[string]map[string]ideSig{},
	}
	workspace.mu.Lock()
	defer workspace.mu.Unlock()
	workspace.timer = time.AfterFunc(ideIdleTTL, workspace.expire)
	workspace.touchLocked()
	if watcher, watcherErr := ideNewWatcher(); watcherErr != nil {
		workspace.fallbackLocked()
	} else {
		workspace.watcher = watcher
		go workspace.loop(watcher, ideDebounce)
	}
	ideWorkspaces[dir] = workspace
	return workspace, nil
}

// ideCloseAll ferme tous les espaces (arrêt du serveur, fin de test) : leurs flux /v1/fs/watch se terminent.
func ideCloseAll() {
	ideWSMu.Lock()
	defer ideWSMu.Unlock()
	for dir, workspace := range ideWorkspaces {
		workspace.mu.Lock()
		workspace.closeLocked()
		workspace.mu.Unlock()
		delete(ideWorkspaces, dir)
	}
}

func (workspace *ideWorkspace) touchLocked() {
	workspace.last = time.Now()
	if workspace.timer != nil {
		workspace.timer.Reset(ideIdleTTL)
	}
}

func (workspace *ideWorkspace) expire() {
	ideWSMu.Lock()
	defer ideWSMu.Unlock()
	workspace.mu.Lock()
	defer workspace.mu.Unlock()
	if workspace.closed || len(workspace.subs) > 0 {
		return
	}
	if remaining := ideIdleTTL - time.Since(workspace.last); remaining > 0 {
		workspace.timer.Reset(remaining)
		return
	}
	workspace.closeLocked()
	if ideWorkspaces[workspace.dir] == workspace {
		delete(ideWorkspaces, workspace.dir)
	}
}

func (workspace *ideWorkspace) closeLocked() {
	if workspace.closed {
		return
	}
	workspace.closed = true
	workspace.timer.Stop()
	if workspace.watcher != nil {
		workspace.watcher.Close()
		workspace.watcher = nil
	}
	workspace.root.Close()
	close(workspace.done)
	// Plus rien n'écrit : flushLocked et fallbackLocked s'arrêtent sur w.closed.
	for subscriber := range workspace.subs {
		close(subscriber.ch)
	}
	clear(workspace.subs)
}

// reopen sert l'appelant qui a obtenu l'espace juste avant sa libération.
func (workspace *ideWorkspace) reopen() *ideWorkspace {
	reopened, err := ideWS(workspace.dir)
	if err != nil {
		return nil
	}
	return reopened
}

func (workspace *ideWorkspace) Index() ([]ideIndexEntry, string) {
	index := workspace.idx()
	return index.entries, index.state
}

// idx lance la construction unique au premier appel, puis une nouvelle après un échec ; ne bloque jamais.
func (workspace *ideWorkspace) idx() ideIdx {
	workspace.mu.Lock()
	if workspace.closed {
		workspace.mu.Unlock()
		if reopened := workspace.reopen(); reopened != nil {
			return reopened.idx()
		}
		return ideIdx{state: "partial"}
	}
	defer workspace.mu.Unlock()
	workspace.touchLocked()
	current := workspace.ix
	if current != nil && !workspace.failed {
		return *current
	}
	if current == nil {
		workspace.ix = &ideIdx{state: "building"}
		current = workspace.ix
	}
	workspace.failed = false
	go workspace.build(workspace.gen)
	return *current
}

// build ne publie que pour sa génération : une invalidation survenue pendant le parcours l'emporte.
func (workspace *ideWorkspace) build(gen int) {
	defer func() {
		if recover() != nil {
			workspace.mu.Lock()
			if workspace.gen == gen {
				workspace.ix, workspace.failed = &ideIdx{state: "partial", set: map[string]bool{}}, true
			}
			workspace.mu.Unlock()
		}
	}()
	ideBuildHook()
	entries, dirs, partial := ideWalk(workspace.root, ideIndexLimits)
	set := make(map[string]bool, len(entries))
	for _, entry := range entries {
		set[entry.Path] = true
	}
	sort.SliceStable(dirs, func(leftIndex, rightIndex int) bool {
		return strings.Count(dirs[leftIndex], "/") < strings.Count(dirs[rightIndex], "/")
	})
	workspace.mu.Lock()
	defer workspace.mu.Unlock()
	if workspace.gen != gen {
		return
	}
	// Au sondage, l'index n'est plus tenu à jour hors des dossiers déclarés.
	state := "ready"
	if partial || workspace.poll {
		state = "partial"
	}
	workspace.ix = &ideIdx{state: state, entries: entries, set: set}
	for _, dir := range dirs {
		if len(workspace.idxDirs) >= ideWatchMax {
			break
		}
		if workspace.watchLocked(dir) {
			workspace.idxDirs[dir] = true
		}
	}
}

// watchLocked n'ajoute que des dossiers qui viennent de passer ideLstat par la racine : fsnotify ouvre par
// chemin absolu, donc un remplacement par un lien entre ce contrôle et l'ajout reste possible.
func (workspace *ideWorkspace) watchLocked(rel string) bool {
	if workspace.watcher == nil || workspace.closed {
		return false
	}
	if workspace.idxDirs[rel] || workspace.declared[rel] > 0 {
		return true
	}
	return workspace.addWatchLocked(rel)
}

// addWatchLocked : Add sur un vrai dossier de l'espace (jamais un lien symbolique), sans regarder s'il est déjà surveillé.
func (workspace *ideWorkspace) addWatchLocked(rel string) bool {
	fileInfo, err := ideLstat(workspace.root, rel)
	if err != nil || !fileInfo.IsDir() {
		return false
	}
	if addErr := ideWatchAdd(workspace.watcher, filepath.Join(workspace.dir, filepath.FromSlash(rel))); addErr != nil {
		// Seules les limites d'inotify condamnent la veille ; un dossier refusé (EACCES…) est seulement sauté.
		isInotifyExhausted := errors.Is(addErr, syscall.ENOSPC) || errors.Is(addErr, syscall.EMFILE) || errors.Is(addErr, syscall.ENFILE)
		if isInotifyExhausted {
			workspace.fallbackLocked()
		}
		return false
	}
	return true
}

func (workspace *ideWorkspace) unwatchLocked(rel string) {
	if workspace.watcher != nil && !workspace.idxDirs[rel] && workspace.declared[rel] == 0 {
		workspace.watcher.Remove(filepath.Join(workspace.dir, filepath.FromSlash(rel)))
	}
}

// lostLocked : des changements ont pu échapper ; les abonnés relisent et le prochain /v1/fs/index reconstruit.
func (workspace *ideWorkspace) lostLocked() {
	workspace.ix = nil
	workspace.gen++
	for subscriber := range workspace.subs {
		subscriber.send(ideResync)
	}
}

// send vide un tampon plein au profit d'un seul resync, qui rend caducs les évènements en attente ; ce resync
// n'est jamais perdu et plusieurs débordements n'en laissent qu'un. Seul flushLocked (sous w.mu) écrit.
func (subscriber *ideSub) send(event ideEvent) {
	select {
	case subscriber.ch <- event:
		return
	default:
	}
	for {
		select {
		case <-subscriber.ch:
			continue
		default:
		}
		break
	}
	subscriber.ch <- ideResync
}

// fallbackLocked : sans inotify (épuisé, débordé, indisponible), seuls les dossiers déclarés sont sondés.
func (workspace *ideWorkspace) fallbackLocked() {
	if workspace.poll || workspace.closed {
		return
	}
	workspace.poll = true
	if workspace.watcher != nil {
		workspace.watcher.Close()
		workspace.watcher = nil
	}
	for declaredDir := range workspace.declared {
		workspace.snaps[declaredDir] = workspace.scanLocked(declaredDir)
	}
	workspace.lostLocked()
	ticker := time.NewTicker(idePollInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-workspace.done:
				return
			case <-ticker.C:
				workspace.pollOnce()
			}
		}
	}()
}

func (workspace *ideWorkspace) scanLocked(rel string) map[string]ideSig {
	signatures := map[string]ideSig{}
	if info, statErr := ideLstat(workspace.root, rel); statErr != nil || !info.IsDir() {
		return signatures
	}
	directory, openErr := workspace.root.Open(rel)
	if openErr != nil {
		return signatures
	}
	dirEntries, _ := directory.ReadDir(-1)
	directory.Close()
	for _, dirEntry := range dirEntries {
		signature := ideSig{dir: dirEntry.IsDir()}
		if info, infoErr := dirEntry.Info(); infoErr == nil {
			signature.mtime, signature.size = info.ModTime().UnixNano(), info.Size()
		}
		signatures[dirEntry.Name()] = signature
	}
	return signatures
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

func (workspace *ideWorkspace) pollOnce() {
	workspace.mu.Lock()
	defer workspace.mu.Unlock()
	var events []ideEvent
	for declaredDir := range workspace.declared {
		previous, current := workspace.snaps[declaredDir], workspace.scanLocked(declaredDir)
		for name, signature := range current {
			if previousSignature, existed := previous[name]; !existed {
				events = append(events, ideEvent{ideJoin(declaredDir, name), "create"})
			} else if previousSignature != signature {
				events = append(events, ideEvent{ideJoin(declaredDir, name), "write"})
			}
		}
		for name := range previous {
			if _, stillExists := current[name]; !stillExists {
				events = append(events, ideEvent{ideJoin(declaredDir, name), "remove"})
			}
		}
		workspace.snaps[declaredDir] = current
	}
	sort.Slice(events, func(leftIndex, rightIndex int) bool {
		return events[leftIndex].Path < events[rightIndex].Path
	})
	workspace.flushLocked(events)
}

func (workspace *ideWorkspace) loop(watcher *fsnotify.Watcher, debounce time.Duration) {
	var pending []ideEvent
	seen := map[ideEvent]bool{}
	var tick <-chan time.Time
	for {
		select {
		case <-workspace.done:
			return
		case fsEvent, eventsOpen := <-watcher.Events:
			if !eventsOpen {
				workspace.mu.Lock()
				workspace.flushLocked(pending)
				workspace.mu.Unlock()
				return
			}
			event, keep := workspace.convert(fsEvent)
			if !keep {
				continue
			}
			if !seen[event] {
				seen[event] = true
				pending = append(pending, event)
			}
			if tick == nil {
				tick = time.After(debounce)
			}
		case err, errorsOpen := <-watcher.Errors:
			if !errorsOpen {
				return
			}
			if errors.Is(err, fsnotify.ErrEventOverflow) {
				workspace.mu.Lock()
				workspace.flushLocked(pending)
				workspace.fallbackLocked()
				workspace.mu.Unlock()
				pending, tick = nil, nil
				clear(seen)
			}
		case <-tick:
			workspace.mu.Lock()
			workspace.flushLocked(pending)
			workspace.mu.Unlock()
			pending, tick = nil, nil
			clear(seen)
		}
	}
}

func (workspace *ideWorkspace) convert(fsEvent fsnotify.Event) (ideEvent, bool) {
	rel, err := filepath.Rel(workspace.dir, fsEvent.Name)
	isOutsideWorkspace := rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
	if err != nil || rel == "." || isOutsideWorkspace {
		return ideEvent{}, false
	}
	event := ideEvent{Path: filepath.ToSlash(rel)}
	switch {
	case fsEvent.Has(fsnotify.Create):
		event.Type = "create"
	case fsEvent.Has(fsnotify.Write):
		event.Type = "write"
	case fsEvent.Has(fsnotify.Remove):
		event.Type = "remove"
	case fsEvent.Has(fsnotify.Rename):
		event.Type = "rename"
	default:
		return ideEvent{}, false
	}
	return event, true
}

func (workspace *ideWorkspace) flushLocked(events []ideEvent) {
	if len(events) == 0 || workspace.closed {
		return
	}
	workspace.applyLocked(events)
	for _, event := range events {
		if event.Type == "create" && workspace.declared[event.Path] > 0 && workspace.watcher != nil {
			// inotify a lâché le dossier quand il a été supprimé : sans nouvel Add, sa recréation (git checkout, rm puis mkdir) reste muette.
			workspace.addWatchLocked(event.Path)
		}
		parent := path.Dir(event.Path)
		for subscriber := range workspace.subs {
			if subscriber.dirs[parent] {
				subscriber.send(event)
			}
		}
	}
}

func ideIndexable(rel string) bool {
	parts := strings.Split(rel, "/")
	for _, part := range parts {
		if ideIgnored(part) {
			return false
		}
	}
	return len(parts)-1 <= ideIndexLimits.Depth
}

// addTreeLocked surveille puis parcourt un dossier arrivé (mkdir suivi d'une écriture rapide, mv d'un dossier
// plein) : dans cet ordre, rien de ce qui y apparaît ensuite n'échappe.
// Le budget (une échéance, les entrées restantes) est commun à tous les dossiers d'un lot : une rafale de
// mkdir ne le multiplie pas. Renvoie false quand le parcours est incomplet.
func (workspace *ideWorkspace) addTreeLocked(rel string, byPath map[string]ideIndexEntry, added map[string]bool, deadline time.Time) bool {
	if len(workspace.idxDirs) < ideWatchMax && workspace.watchLocked(rel) {
		workspace.idxDirs[rel] = true
	}
	limits := ideLimits{
		Entries: ideIndexLimits.Entries - len(byPath),
		Depth:   ideIndexLimits.Depth - strings.Count(rel, "/") - 1,
		Time:    time.Until(deadline),
	}
	if limits.Entries <= 0 || limits.Depth < 0 || limits.Time <= 0 {
		return false
	}
	subRoot, err := workspace.root.OpenRoot(rel)
	if err != nil {
		return true
	}
	defer subRoot.Close()
	entries, dirs, partial := ideWalk(subRoot, limits)
	for _, entry := range entries {
		entry.Path = rel + "/" + entry.Path
		byPath[entry.Path] = entry
		added[entry.Path] = true
	}
	for _, walkedDir := range dirs {
		walkedDir = ideJoin(rel, walkedDir)
		if walkedDir != rel && len(workspace.idxDirs) < ideWatchMax && workspace.watchLocked(walkedDir) {
			workspace.idxDirs[walkedDir] = true
		}
	}
	return !partial
}

// applyLocked met l'index à jour par copie, l'ancien instantané restant lisible par les appelants.
func (workspace *ideWorkspace) applyLocked(events []ideEvent) {
	if workspace.ix == nil || workspace.ix.state == "building" {
		return
	}
	byPath := make(map[string]ideIndexEntry, len(workspace.ix.entries)+len(events))
	for _, entry := range workspace.ix.entries {
		byPath[entry.Path] = entry
	}
	state := workspace.ix.state
	old := workspace.ix.entries // trié : les descendants d'un dossier y forment une plage contiguë
	added := map[string]bool{}
	// ponytail: parcours sous verrou borné par ideTreeBudget et par lot ; un gros dossier déplacé fait attendre /v1/fs/index d'autant
	deadline := time.Now().Add(ideTreeBudget)
	for _, event := range events {
		if !ideIndexable(event.Path) {
			continue
		}
		fileInfo, err := ideLstat(workspace.root, event.Path)
		switch {
		case err != nil || event.Type == "remove" || event.Type == "rename":
			delete(byPath, event.Path)
			delete(added, event.Path)
			prefix := event.Path + "/"
			firstDescendant := sort.Search(len(old), func(searchIndex int) bool {
				return old[searchIndex].Path >= prefix
			})
			for descendantIndex := firstDescendant; descendantIndex < len(old); descendantIndex++ {
				if !strings.HasPrefix(old[descendantIndex].Path, prefix) {
					break
				}
				delete(byPath, old[descendantIndex].Path)
			}
			// Seul un dossier surveillé a pu recevoir des entrées pendant ce lot ou avoir des sous-dossiers surveillés.
			if workspace.idxDirs[event.Path] {
				for addedPath := range added {
					if strings.HasPrefix(addedPath, prefix) {
						delete(byPath, addedPath)
						delete(added, addedPath)
					}
				}
				for watchedDir := range workspace.idxDirs {
					if watchedDir == event.Path || strings.HasPrefix(watchedDir, prefix) {
						delete(workspace.idxDirs, watchedDir)
					}
				}
			}
		case fileInfo.IsDir():
			if !workspace.addTreeLocked(event.Path, byPath, added, deadline) {
				state = "partial"
			}
		case fileInfo.Mode().IsRegular():
			if _, indexed := byPath[event.Path]; indexed || len(byPath) < ideIndexLimits.Entries {
				byPath[event.Path] = ideIndexEntry{Path: event.Path, Size: fileInfo.Size(), Mtime: fileInfo.ModTime().Unix()}
				added[event.Path] = true
			}
		}
	}
	entries := make([]ideIndexEntry, 0, len(byPath))
	set := make(map[string]bool, len(byPath))
	for entryPath, entry := range byPath {
		entries = append(entries, entry)
		set[entryPath] = true
	}
	sort.Slice(entries, func(leftIndex, rightIndex int) bool {
		return entries[leftIndex].Path < entries[rightIndex].Path
	})
	workspace.ix = &ideIdx{state: state, entries: entries, set: set}
}

func (workspace *ideWorkspace) Subscribe(paths []string) (<-chan ideEvent, func()) {
	workspace.mu.Lock()
	if workspace.closed {
		workspace.mu.Unlock()
		if reopened := workspace.reopen(); reopened != nil {
			return reopened.Subscribe(paths)
		}
		return make(chan ideEvent), func() {}
	}
	defer workspace.mu.Unlock()
	subscriber := &ideSub{dirs: map[string]bool{}, ch: make(chan ideEvent, ideSubBuf)}
	for _, requestedPath := range paths {
		if len(subscriber.dirs) >= ideSubPaths {
			break
		}
		rel, err := ideRel(requestedPath, true)
		if err != nil || subscriber.dirs[rel] {
			continue
		}
		if fileInfo, statErr := ideLstat(workspace.root, rel); statErr != nil || !fileInfo.IsDir() {
			continue
		}
		if !workspace.poll && !workspace.watchLocked(rel) && !workspace.poll {
			continue
		}
		if workspace.poll && workspace.declared[rel] == 0 {
			workspace.snaps[rel] = workspace.scanLocked(rel)
		}
		subscriber.dirs[rel] = true
		workspace.declared[rel]++
	}
	workspace.subs[subscriber] = true
	var once sync.Once
	return subscriber.ch, func() {
		once.Do(func() {
			workspace.mu.Lock()
			defer workspace.mu.Unlock()
			delete(workspace.subs, subscriber)
			for dir := range subscriber.dirs {
				if workspace.declared[dir]--; workspace.declared[dir] <= 0 {
					delete(workspace.declared, dir)
					delete(workspace.snaps, dir)
					workspace.unwatchLocked(dir)
				}
			}
			if !workspace.closed {
				workspace.touchLocked()
			}
		})
	}
}

func routesIDEWatch(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/fs/watch", func(responseWriter http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		dir, known := ideKnownDir(query.Get("dir"))
		if !known {
			ideFail(responseWriter, errIdeDir)
			return
		}
		workspace, err := ideWS(dir)
		if err != nil {
			ideFail(responseWriter, err)
			return
		}
		events, cancel := workspace.Subscribe(query["paths"])
		defer cancel()
		responseWriter.Header().Set("Content-Type", "text/event-stream")
		responseWriter.Header().Set("Cache-Control", "no-store")
		responseWriter.Header().Set("X-Accel-Buffering", "no")
		responseWriter.WriteHeader(http.StatusOK)
		responseController := http.NewResponseController(responseWriter)
		responseController.Flush()
		ping := time.NewTicker(idePing)
		defer ping.Stop()
		for {
			var frame []byte
			select {
			case <-request.Context().Done():
				return
			case event, isOpen := <-events:
				if !isOpen {
					return
				}
				encoded, _ := json.Marshal(event)
				frame = append([]byte("data: "), encoded...)
				frame = append(frame, "\n\n"...)
			case <-ping.C:
				frame = []byte(": ping\n\n")
			}
			if _, sendErr := responseWriter.Write(frame); sendErr != nil || responseController.Flush() != nil {
				return
			}
		}
	})
}
