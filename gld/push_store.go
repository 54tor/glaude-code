package main

// Web Push : clés VAPID du serveur et abonnements des appareils, dans un seul fichier du dossier de configuration de la passerelle.

import (
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"sync"

	webpush "github.com/SherClockHolmes/webpush-go"
)

const (
	pushSubscriptionsMax     = 200
	pushSubscriptionsPerUser = 10
)

type pushSubscription struct {
	Endpoint string `json:"endpoint"`
	P256dh   string `json:"p256dh"`
	Auth     string `json:"auth"`
	UserID   int64  `json:"userId"`
	// SessionID : la session qui a créé l'abonnement ; avec comptes, l'abonnement ne sert plus dès qu'elle est fermée.
	SessionID string `json:"sessionId,omitempty"`
	Lang      string `json:"lang"`
	Asking    bool   `json:"asking"`
	Waiting   bool   `json:"waiting"`
}

type pushVapid struct {
	Public  string `json:"public"`
	Private string `json:"private"`
}

type pushFileContent struct {
	Vapid         pushVapid          `json:"vapid"`
	Subscriptions []pushSubscription `json:"subscriptions"`
}

var pushStoreMu sync.Mutex

func pushFile() string { return filepath.Join(cfgDir(), "gld-web-push.json") }

// pushLoad : un fichier absent est vide ; un fichier illisible est une erreur, jamais réécrite par-dessus.
func pushLoad() (pushFileContent, error) {
	var content pushFileContent
	rawContent, readErr := readTrusted(pushFile())
	if errors.Is(readErr, fs.ErrNotExist) {
		return content, nil
	}
	if readErr != nil {
		return pushFileContent{}, terr("srv.push.fichierIllisible", "A1", readErr.Error())
	}
	if decodeErr := json.Unmarshal(rawContent, &content); decodeErr != nil {
		return pushFileContent{}, terr("srv.push.fichierIllisible", "A1", decodeErr.Error())
	}
	return content, nil
}

func pushSave(content pushFileContent) error {
	encoded, _ := json.MarshalIndent(content, "", "  ")
	return rWriteAtomic(cfgDir(), pushFile(), encoded, 0o600)
}

func pushVapidKeys() (publicKey, privateKey string, err error) {
	pushStoreMu.Lock()
	defer pushStoreMu.Unlock()
	content, loadErr := pushLoad()
	if loadErr != nil {
		return "", "", loadErr
	}
	if content.Vapid.Public != "" && content.Vapid.Private != "" {
		return content.Vapid.Public, content.Vapid.Private, nil
	}
	generatedPrivate, generatedPublic, generateErr := webpush.GenerateVAPIDKeys()
	if generateErr != nil {
		return "", "", generateErr
	}
	content.Vapid = pushVapid{Public: generatedPublic, Private: generatedPrivate}
	if saveErr := pushSave(content); saveErr != nil {
		return "", "", saveErr
	}
	return generatedPublic, generatedPrivate, nil
}

func pushSubscriptionsLoad() ([]pushSubscription, error) {
	pushStoreMu.Lock()
	defer pushStoreMu.Unlock()
	content, loadErr := pushLoad()
	return content.Subscriptions, loadErr
}

func pushSubscriptionFind(endpoint string) (pushSubscription, bool, error) {
	subscriptions, loadErr := pushSubscriptionsLoad()
	if loadErr != nil {
		return pushSubscription{}, false, loadErr
	}
	for _, subscription := range subscriptions {
		if subscription.Endpoint == endpoint {
			return subscription, true, nil
		}
	}
	return pushSubscription{}, false, nil
}

func pushSubscriptionUpsert(newSubscription pushSubscription) error {
	pushStoreMu.Lock()
	defer pushStoreMu.Unlock()
	content, loadErr := pushLoad()
	if loadErr != nil {
		return loadErr
	}
	content.Subscriptions = slices.DeleteFunc(content.Subscriptions, func(existingSubscription pushSubscription) bool {
		return existingSubscription.UserID == newSubscription.UserID && !pushSessionLive(existingSubscription.SessionID)
	})
	replaced := false
	userSubscriptionCount := 0
	for subscriptionIndex, existingSubscription := range content.Subscriptions {
		if existingSubscription.Endpoint == newSubscription.Endpoint {
			content.Subscriptions[subscriptionIndex] = newSubscription
			replaced = true
		} else if existingSubscription.UserID == newSubscription.UserID {
			userSubscriptionCount++
		}
	}
	if !replaced {
		if len(content.Subscriptions) >= pushSubscriptionsMax || userSubscriptionCount >= pushSubscriptionsPerUser {
			return terr("srv.push.trop")
		}
		content.Subscriptions = append(content.Subscriptions, newSubscription)
	}
	return pushSave(content)
}

func pushSubscriptionDelete(endpoint string) error {
	pushStoreMu.Lock()
	defer pushStoreMu.Unlock()
	content, loadErr := pushLoad()
	if loadErr != nil {
		return loadErr
	}
	keptSubscriptions := content.Subscriptions[:0]
	for _, subscription := range content.Subscriptions {
		if subscription.Endpoint != endpoint {
			keptSubscriptions = append(keptSubscriptions, subscription)
		}
	}
	content.Subscriptions = keptSubscriptions
	return pushSave(content)
}
