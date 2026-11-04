package main

// Web Push : clés VAPID du serveur et abonnements des appareils, dans un seul fichier du dossier de configuration de la passerelle.

import (
	"encoding/json"
	"path/filepath"
	"sync"

	webpush "github.com/SherClockHolmes/webpush-go"
)

const pushSubscriptionsMax = 200

type pushSubscription struct {
	Endpoint string `json:"endpoint"`
	P256dh   string `json:"p256dh"`
	Auth     string `json:"auth"`
	UserID   int64  `json:"userId"`
	Lang     string `json:"lang"`
	Asking   bool   `json:"asking"`
	Waiting  bool   `json:"waiting"`
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
	if readErr != nil {
		return content, nil
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
	replaced := false
	for subscriptionIndex, existingSubscription := range content.Subscriptions {
		if existingSubscription.Endpoint == newSubscription.Endpoint {
			content.Subscriptions[subscriptionIndex] = newSubscription
			replaced = true
		}
	}
	if !replaced {
		if len(content.Subscriptions) >= pushSubscriptionsMax {
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
