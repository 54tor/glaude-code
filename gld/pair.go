package main

// Jumelage d'un appareil. Un navigateur déjà connecté demande un code à usage unique (huit chiffres, deux minutes) et
// l'affiche en clair et en QR. Le téléphone le saisit sur « Connecter cet appareil » (/pair, liée depuis la page du jeton
// et l'écran de connexion) : la session naît là où il le tape, donc dans la PWA. Le QR ouvre /pair?c=…, une page de
// confirmation sans effet (un aperçu de lien ne consomme rien) dont le bouton envoie le même POST de même origine.
// Les codes faux comptent dans les essais du jeton (tokenTry) : 10^8 codes, deux minutes, 10 essais par minute.
// Mode jeton : session d'appareil (devices.json, 90 jours sans visite), liée au jeton : en changer coupe tous les appareils.
// Mode comptes : session du compte dans auth.db, second facteur acquis, tant que la session qui a émis le code vit.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	mbig "math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	pairTTL   = 2 * time.Minute
	devIdle   = 90 * 24 * time.Hour
	devTouch  = time.Minute
	devCookie = "gld_dev"

	pairCodeSpace            = 1e8
	deviceSecretBytes        = 32
	deviceUserAgentMaxLength = 256
	deviceCookieMaxAge       = 400 * 24 * 3600
	pairFormMaxBytes         = 1 << 12
)

type pairCode struct {
	exp time.Time
	uid int64  // 0 : mode jeton
	sid string // session émettrice (mode comptes)
}

var (
	pairMu    sync.Mutex
	pairCodes = map[string]pairCode{}
)

// Codes en attente : au plus pairMaxPerSession par compte (toutes sessions confondues, une seule file en mode jeton) et pairMaxTotal au total (deux minutes de vie).
const (
	pairMaxPerSession = 5
	pairMaxTotal      = 200
)

// newPairCode : "" quand le plafond est atteint.
func newPairCode(userID int64, sessionID string) string {
	now := authNow()
	pairMu.Lock()
	defer pairMu.Unlock()
	ownedCount := 0
	for code, pending := range pairCodes {
		if now.After(pending.exp) {
			delete(pairCodes, code)
		} else if pending.uid == userID {
			ownedCount++
		}
	}
	if ownedCount >= pairMaxPerSession || len(pairCodes) >= pairMaxTotal {
		return ""
	}
	for {
		randomNumber, _ := rand.Int(rand.Reader, mbig.NewInt(pairCodeSpace))
		candidate := fmt.Sprintf("%08d", randomNumber)
		if _, used := pairCodes[candidate]; !used {
			pairCodes[candidate] = pairCode{now.Add(pairTTL), userID, sessionID}
			return candidate
		}
	}
}

// pairDigits : le code tel que tapé (« 1234 5678 », « 1234-5678 »), réduit à ses chiffres.
func pairDigits(typedCode string) string {
	return strings.Map(func(character rune) rune {
		if character >= '0' && character <= '9' {
			return character
		}
		return -1
	}, typedCode)
}

// pairLookup lit le code ; take le consomme, valide ou non : un code essayé ne sert plus.
func pairLookup(code string, take bool) (pairCode, bool) {
	pairMu.Lock()
	defer pairMu.Unlock()
	pending, found := pairCodes[code]
	if take {
		delete(pairCodes, code)
	}
	return pending, found && authNow().Before(pending.exp)
}

// device : un appareil jumelé en mode jeton. Le cookie n'est jamais stocké, seulement son HMAC sous la clé du jeton.
type device struct {
	ID      string `json:"id"`
	Bind    string `json:"bind"` // empreinte de la clé du jeton : un autre jeton ne reconnaît plus l'appareil
	Name    string `json:"name"`
	IP      string `json:"ip"`
	Created int64  `json:"created"`
	Seen    int64  `json:"seen"`
}

var (
	devMu  sync.Mutex
	devKey []byte // dérivée du jeton par withToken ; nil sans jeton : rien à jumeler
)

func setDevKey(token string) {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte("gld-device"))
	devMu.Lock()
	devKey = mac.Sum(nil)
	devMu.Unlock()
}

// devMac et devBind supposent devMu tenu.
func devMac(cookie string) string {
	mac := hmac.New(sha256.New, devKey)
	mac.Write([]byte(cookie))
	return hex.EncodeToString(mac.Sum(nil))
}

func devBind() string {
	keyHash := sha256.Sum256(devKey)
	return hex.EncodeToString(keyHash[:8])
}

func devicesFile() (string, error) {
	directory, err := authDir()
	return filepath.Join(directory, "devices.json"), err
}

func loadDevices() ([]device, error) {
	path, err := devicesFile()
	if err != nil {
		return nil, err
	}
	content, err := readTrusted(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var devices []device
	return devices, json.Unmarshal(content, &devices)
}

func saveDevices(devices []device) error {
	path, err := devicesFile()
	if err != nil {
		return err
	}
	if mkdirErr := os.MkdirAll(filepath.Dir(path), 0o700); mkdirErr != nil {
		return mkdirErr
	}
	content, _ := json.MarshalIndent(devices, "", "  ")
	tempPath := path + ".tmp"
	if fileWriteErr := os.WriteFile(tempPath, content, 0o600); fileWriteErr != nil {
		return fileWriteErr
	}
	return os.Rename(tempPath, path)
}

// liveDevices : les appareils du jeton courant vus depuis moins de devIdle (devMu tenu).
func liveDevices(devices []device) []device {
	bind := devBind()
	return slices.DeleteFunc(unexpired(devices), func(candidate device) bool { return candidate.Bind != bind })
}

// unexpired : ce qui reste dans devices.json. Un appareil d'un autre jeton est refusé, pas effacé : un second gld lancé avec un
// autre jeton sur le même dossier ne doit pas faire perdre ses appareils au premier. Il part seul après devIdle.
func unexpired(devices []device) []device {
	limit := authNow().Add(-devIdle).Unix()
	return slices.DeleteFunc(devices, func(candidate device) bool { return candidate.Seen < limit })
}

func addDevice(userAgent, clientAddress string) (string, error) {
	secret := make([]byte, deviceSecretBytes)
	rand.Read(secret)
	cookie := base64.RawURLEncoding.EncodeToString(secret)
	if len(userAgent) > deviceUserAgentMaxLength {
		userAgent = userAgent[:deviceUserAgentMaxLength]
	}
	now := authNow().Unix()
	devMu.Lock()
	defer devMu.Unlock()
	if devKey == nil {
		return "", errors.New("pas de jeton")
	}
	devices, err := loadDevices()
	if err != nil {
		return "", err
	}
	return cookie, saveDevices(append(unexpired(devices), device{devMac(cookie), devBind(), userAgent, clientAddress, now, now}))
}

// deviceOK admet un cookie d'appareil encore vivant et repousse son échéance (au plus une écriture par minute).
func deviceOK(cookie string) bool {
	now := authNow().Unix()
	devMu.Lock()
	defer devMu.Unlock()
	if devKey == nil {
		return false
	}
	devices, err := loadDevices()
	if err != nil {
		journal("pair: devices.json illisible : %v", err)
		return false
	}
	countBefore, cookieMac, bind := len(devices), devMac(cookie), devBind()
	devices = unexpired(devices)
	deviceIndex := slices.IndexFunc(devices, func(candidate device) bool {
		return candidate.Bind == bind && hmac.Equal([]byte(candidate.ID), []byte(cookieMac))
	})
	isKnownDevice := deviceIndex >= 0
	dirty := len(devices) != countBefore
	if isKnownDevice && now-devices[deviceIndex].Seen >= int64(devTouch/time.Second) {
		devices[deviceIndex].Seen, dirty = now, true
	}
	if dirty {
		if saveErr := saveDevices(devices); saveErr != nil {
			journal("pair: devices.json : %v", saveErr)
		}
	}
	return isKnownDevice
}

var pairCOP = http.NewCrossOriginProtection()

// pairEntry : GET /pair (confirmation, sans effet) et POST /pair (création de la session). store nil : mode jeton.
func pairEntry(responseWriter http.ResponseWriter, request *http.Request, store *authStore) {
	devMu.Lock()
	keyless := devKey == nil
	devMu.Unlock()
	if store == nil && keyless {
		http.Redirect(responseWriter, request, "/", http.StatusSeeOther)
		return
	}
	form := func(code int, errKey string) {
		page := gateData{Title: L("srv.pair.title"), Text: L("srv.pair.text"), Action: "/pair", Code: true, Button: L("srv.pair.button"), Link: "/", LinkText: L("srv.pair.back")}
		if errKey != "" {
			page.Err = L(errKey)
		}
		gatePage(responseWriter, code, page)
	}
	replyExpired := func() { form(http.StatusGone, "srv.pair.expired") }
	clientAddress := clientIP(request)
	if request.Method == "GET" && request.URL.Query().Get("c") == "" {
		form(200, "")
		return
	}
	if request.Method == "POST" && pairCOP.Check(request) != nil {
		http.Error(responseWriter, "forbidden", http.StatusForbidden)
		return
	}
	if !tokenTry(clientAddress) {
		responseWriter.Header().Set("Retry-After", "60")
		form(http.StatusTooManyRequests, "srv.token.tooMany")
		return
	}
	if request.Method == "GET" {
		code := pairDigits(request.URL.Query().Get("c"))
		pending, found := pairLookup(code, false)
		if !found || (pending.uid != 0) != (store != nil) {
			replyExpired()
			return
		}
		tokenRefund(clientAddress)
		text := L("srv.pair.textToken")
		if store != nil {
			user, userErr := store.userByID(store.db, pending.uid)
			if userErr != nil {
				replyExpired()
				return
			}
			text = L("srv.pair.textAccount", "A1", user.Name)
		}
		gatePage(responseWriter, 200, gateData{Title: L("srv.pair.title"), Text: text, Action: "/pair", Hidden: code, Button: L("srv.pair.button")})
		return
	}
	request.Body = http.MaxBytesReader(responseWriter, request.Body, pairFormMaxBytes)
	pending, found := pairLookup(pairDigits(request.PostFormValue("c")), true)
	if !found || (pending.uid != 0) != (store != nil) {
		replyExpired()
		return
	}
	tokenRefund(clientAddress)
	if store == nil {
		cookie, addErr := addDevice(request.UserAgent(), clientAddress)
		if addErr != nil {
			journal("pair: %v", addErr)
			replyExpired()
			return
		}
		// MaxAge large : l'échéance réelle (90 jours sans visite) est tenue côté serveur.
		http.SetCookie(responseWriter, &http.Cookie{
			Name:     devCookie,
			Value:    cookie,
			Path:     "/",
			MaxAge:   deviceCookieMaxAge,
			HttpOnly: true,
			Secure:   requestHTTPS(request),
			SameSite: http.SameSiteStrictMode,
		})
		journal("pair: appareil jumelé depuis %s", clientAddress)
	} else if !redeemSession(responseWriter, request, store, pending, clientAddress) {
		replyExpired()
		return
	}
	http.Redirect(responseWriter, request, "/", http.StatusSeeOther)
}

// redeemSession : la session naît complète (second facteur acquis), à condition que la session émettrice soit encore
// ouverte et complète : la révoquer annule aussi ses codes en attente.
func redeemSession(responseWriter http.ResponseWriter, request *http.Request, store *authStore, pending pairCode, clientAddress string) bool {
	cookie, expiry, err := store.NewPairedSession(pending.uid, pending.sid, clientAddress, request.UserAgent())
	if err == nil {
		if _, err = store.Session(cookie); err != nil { // refuse un compte désactivé depuis
			if rawCookie, decodeErr := base64.RawURLEncoding.DecodeString(cookie); decodeErr == nil {
				store.RevokeSession(hex.EncodeToString(store.sessHash(rawCookie)))
			}
		}
	}
	if err != nil {
		if !errors.Is(err, errNoSession) {
			journal("pair: session : %v", err)
		}
		return false
	}
	setSessionCookie(responseWriter, request, cookie, expiry)
	clearHomeCookie(responseWriter)
	store.eventIP(pending.uid, evLogin, "pair", clientAddress)
	return true
}

// publicBase : l'adresse que le téléphone ouvrira. GLD_PUBLIC_URL (--public-url) d'abord : l'ordinateur qui affiche le QR
// est souvent sur 127.0.0.1 ou l'IP locale ; sinon, l'adresse par laquelle la requête est arrivée.
func publicBase(request *http.Request) string {
	publicURL, err := url.Parse(strings.TrimSpace(os.Getenv("GLD_PUBLIC_URL")))
	hasWebScheme := err == nil && (publicURL.Scheme == "http" || publicURL.Scheme == "https")
	if hasWebScheme && publicURL.Host != "" {
		return publicURL.Scheme + "://" + publicURL.Host
	}
	if requestHTTPS(request) {
		return "https://" + request.Host
	}
	return "http://" + request.Host
}

func routesPair(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/pairing/codes", func(responseWriter http.ResponseWriter, request *http.Request) {
		request, mode := reqMode(request)
		var userID int64
		var sessionID string
		if mode.on {
			gate, _ := request.Context().Value(gateKey{}).(*gateInfo)
			if gate == nil || gate.p == nil || gate.p.User == nil || !gate.p.MFA {
				writeErr(responseWriter, 403, "forbidden", L("srv.auth.droitRefuse"))
				return
			}
			userID, sessionID = gate.p.User.ID, gate.p.SessionID
		}
		code := newPairCode(userID, sessionID)
		if code == "" {
			responseWriter.Header().Set("Retry-After", "60")
			writeErr(responseWriter, 429, "rate_limited", L("srv.token.tooMany"))
			return
		}
		writeJSON(responseWriter, 201, map[string]any{"code": code, "url": publicBase(request) + "/pair?c=" + code, "ttl": int(pairTTL / time.Second)})
	})
	// Mode comptes, ou sans jeton ; en mode jeton, withToken répond avant.
	entry := func(responseWriter http.ResponseWriter, request *http.Request) {
		request, mode := reqMode(request)
		var store *authStore
		if mode.on {
			store = mode.st
		}
		pairEntry(responseWriter, request, store)
	}
	mux.HandleFunc("GET /pair", entry)
	mux.HandleFunc("POST /pair", entry)
	// Mode jeton seulement : en mode comptes, les sessions du compte (/v1/auth/sessions) en tiennent lieu.
	mux.HandleFunc("GET /v1/devices", func(responseWriter http.ResponseWriter, request *http.Request) {
		if _, mode := reqMode(request); mode.on {
			http.NotFound(responseWriter, request)
			return
		}
		devMu.Lock()
		devices, err := loadDevices()
		if err == nil {
			devices = liveDevices(devices)
		}
		devMu.Unlock()
		if err != nil {
			writeErr(responseWriter, 500, "internal", err.Error())
			return
		}
		writeJSON(responseWriter, 200, map[string]any{"devices": devices})
	})
	mux.HandleFunc("DELETE /v1/devices/{device}", func(responseWriter http.ResponseWriter, request *http.Request) {
		if _, mode := reqMode(request); mode.on {
			http.NotFound(responseWriter, request)
			return
		}
		deviceID := request.PathValue("device")
		devMu.Lock()
		defer devMu.Unlock()
		devices, err := loadDevices()
		if err == nil {
			err = saveDevices(slices.DeleteFunc(devices, func(candidate device) bool { return candidate.ID == deviceID }))
		}
		if err != nil {
			writeErr(responseWriter, 500, "internal", err.Error())
			return
		}
		journal("pair: appareil %s révoqué", cut(deviceID, 8))
		responseWriter.WriteHeader(http.StatusNoContent)
	})
}
