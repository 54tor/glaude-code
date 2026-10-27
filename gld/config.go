package main

// Configuration de Claude Code : réglages utilisateur (~/.claude/settings.json), de projet (.claude/settings.json) et
// locaux (.claude/settings.local.json). Lecture, masquage des secrets, et JSON ordonné : une écriture ne doit jamais
// réordonner ni reformater le fichier de l'utilisateur.

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// --- JSON ordonné ------------------------------------------------------------------------------------

// omap : un objet JSON qui garde l'ordre de ses clés.
type omap struct {
	keys []string
	m    map[string]any
}

func newOmap() *omap { return &omap{m: map[string]any{}} }

func (o *omap) get(k string) (any, bool) { v, ok := o.m[k]; return v, ok }

func (o *omap) set(k string, v any) {
	if _, ok := o.m[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.m[k] = v
}

func (o *omap) del(k string) {
	if _, ok := o.m[k]; !ok {
		return
	}
	delete(o.m, k)
	for i, x := range o.keys {
		if x == k {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
}

// parseOrdered décode un document JSON en conservant l'ordre des clés (nombres gardés tels quels : json.Number).
func parseOrdered(b []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	v, err := decodeValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, terr("srv.config.texteApresLaFin")
	}
	return v, nil
}

func decodeValue(dec *json.Decoder) (any, error) {
	t, err := dec.Token()
	if err != nil {
		return nil, err
	}
	d, ok := t.(json.Delim)
	if !ok {
		return t, nil
	}
	if d == '{' {
		o := newOmap()
		for dec.More() {
			kt, err := dec.Token()
			if err != nil {
				return nil, err
			}
			v, err := decodeValue(dec)
			if err != nil {
				return nil, err
			}
			o.set(kt.(string), v)
		}
		_, err := dec.Token()
		return o, err
	}
	arr := []any{}
	for dec.More() {
		v, err := decodeValue(dec)
		if err != nil {
			return nil, err
		}
		arr = append(arr, v)
	}
	_, err = dec.Token()
	return arr, err
}

func encodeCompact(v any, w *bytes.Buffer) {
	switch x := v.(type) {
	case *omap:
		w.WriteByte('{')
		for i, k := range x.keys {
			if i > 0 {
				w.WriteByte(',')
			}
			encodeCompact(k, w)
			w.WriteByte(':')
			encodeCompact(x.m[k], w)
		}
		w.WriteByte('}')
	case []any:
		w.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				w.WriteByte(',')
			}
			encodeCompact(e, w)
		}
		w.WriteByte(']')
	default:
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		enc.Encode(x)
		w.Truncate(w.Len() - 1) // le « \n » de Encode
	}
}

// formatOrdered : le document en JSON indenté de deux espaces, comme Claude Code l'écrit ; sans « \n » final.
func formatOrdered(v any) string {
	var c bytes.Buffer
	encodeCompact(v, &c)
	var out bytes.Buffer
	json.Indent(&out, c.Bytes(), "", "  ")
	return out.String()
}

// --- fichiers de réglages -----------------------------------------------------------------------------

// ConfigFile : un fichier de réglages. id : « u » (utilisateur), « p/<dossier> » (projet), « l/<dossier> » (local).
type ConfigFile struct {
	ID     string `json:"id"`
	Scope  string `json:"scope"` // user, project, local
	Dir    string `json:"dir,omitempty"`
	Label  string `json:"label"`
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	Error  string `json:"error,omitempty"`
	Data   any    `json:"data,omitempty"` // contenu, secrets masqués
}

// configPath traduit un identifiant en chemin, après validation stricte (le projet doit exister).
func configPath(id string) (string, error) {
	switch {
	case id == "u":
		return filepath.Join(cfgDir(), "settings.json"), nil
	case strings.HasPrefix(id, "p/"), strings.HasPrefix(id, "l/"):
		dir := id[2:]
		if !safeDir(dir) {
			return "", terr("srv.memmeta.projetInvalide")
		}
		cwd := projectCwd(dir)
		if fi, err := os.Stat(cwd); cwd == "" || err != nil || !fi.IsDir() {
			return "", terr("srv.memory.dossierDuProjetIntrouvable")
		}
		name := "settings.json"
		if id[0] == 'l' {
			name = "settings.local.json"
		}
		return filepath.Join(cwd, ".claude", name), nil
	}
	return "", terr("srv.config.fichierDeReglagesInvalide")
}

var secretKey = regexp.MustCompile(`(?i)(key|token|secret|passw|credential|auth|cookie|bearer)`)

// maskSecrets remplace par des points la valeur texte de toute clé qui ressemble à un secret.
func maskSecrets(v any, key string) any {
	switch x := v.(type) {
	case *omap:
		out := newOmap()
		for _, k := range x.keys {
			out.set(k, maskSecrets(x.m[k], k))
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = maskSecrets(e, key)
		}
		return out
	case string:
		if secretKey.MatchString(key) && x != "" {
			return "••••••••"
		}
	}
	return v
}

// readConfig lit un fichier de réglages ; un fichier absent est vide, pas une erreur.
func readConfig(id string) (ConfigFile, any) {
	f := ConfigFile{ID: id}
	switch id[0] {
	case 'u':
		f.Scope, f.Label = "user", "~/.claude/settings.json"
	case 'p':
		f.Scope, f.Dir = "project", id[2:]
	default:
		f.Scope, f.Dir = "local", id[2:]
	}
	p, err := configPath(id)
	if err != nil {
		f.Error = err.Error()
		return f, nil
	}
	f.Path = p
	if f.Dir != "" {
		f.Label = ".claude/" + filepath.Base(p)
	}
	b, err := readTrusted(p)
	if err != nil {
		return f, nil
	}
	f.Exists = true
	v, err := parseOrdered(b)
	if err != nil {
		f.Error = L("srv.configraw.jsonInvalide", "A1", err.Error())
		return f, nil
	}
	if _, ok := v.(*omap); !ok {
		f.Error = "le fichier doit contenir un objet JSON"
		return f, nil
	}
	return f, v
}

// configProjects : les projets qui ont leurs propres réglages. ~/.claude (le dossier de configuration lui-même) et ~ n'en sont pas :
// les réglages « de projet » de ~ sont ~/.claude/settings.json, donc le fichier utilisateur, déjà affiché à part.
func configProjects() []MemTarget {
	home, _ := os.UserHomeDir()
	out := []MemTarget{}
	for _, p := range liveProjects() {
		if p.Project != cfgDir() && (home == "" || p.Project != home) {
			out = append(out, p)
		}
	}
	return out
}

// allConfigs : le fichier utilisateur, puis, pour chaque projet dont le dossier existe, ses fichiers de projet et local
// quand ils existent. raw garde les secrets (pour les analyses), data les masque (pour l'affichage).
func allConfigs() (files []ConfigFile, raw map[string]any) {
	raw = map[string]any{}
	add := func(id string, onlyExisting bool) {
		f, v := readConfig(id)
		if onlyExisting && !f.Exists {
			return
		}
		if v != nil {
			raw[id] = v
			f.Data = maskSecrets(v, "")
		}
		files = append(files, f)
	}
	add("u", false)
	for _, p := range configProjects() {
		add("p/"+p.Dir, true)
		add("l/"+p.Dir, true)
	}
	return files, raw
}

// --- accès aux valeurs --------------------------------------------------------------------------------

func asMap(v any) *omap { o, _ := v.(*omap); return o }

// lookup : la valeur à un chemin de clés (objets seulement).
func lookup(doc any, path ...string) (any, bool) {
	cur := doc
	for _, k := range path {
		o := asMap(cur)
		if o == nil {
			return nil, false
		}
		v, ok := o.get(k)
		if !ok {
			return nil, false
		}
		cur = v
	}
	return cur, true
}

func strList(v any) []string {
	var out []string
	if a, ok := v.([]any); ok {
		for _, e := range a {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// MarshalJSON : l'ordre des clés est conservé dans les réponses de l'API.
func (o *omap) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	encodeCompact(o, &b)
	return b.Bytes(), nil
}
