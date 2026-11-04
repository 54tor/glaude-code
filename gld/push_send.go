package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

const (
	pushSubscriber = "https://github.com/54tor/glaude-code"
	pushTTLSeconds = 3600

	pushSendTimeout = 15 * time.Second
)

var pushHTTPClient = &http.Client{
	Timeout:       pushSendTimeout,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

type pushEvent struct {
	HomeID    string
	SessionID string
	Title     string
	Kind      string // "asking" ou "waiting"
}

type pushPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Tag   string `json:"tag"`
	URL   string `json:"url"`
}

var (
	pushTransport = webpushSend
	pushMayRead   = pushUserMayRead
)

func webpushSend(subscription pushSubscription, payload []byte) (int, error) {
	publicKey, privateKey, keysErr := pushVapidKeys()
	if keysErr != nil {
		return 0, keysErr
	}
	response, sendErr := webpush.SendNotification(payload, &webpush.Subscription{
		Endpoint: subscription.Endpoint,
		Keys:     webpush.Keys{Auth: subscription.Auth, P256dh: subscription.P256dh},
	}, &webpush.Options{
		Subscriber:      pushSubscriber,
		VAPIDPublicKey:  publicKey,
		VAPIDPrivateKey: privateKey,
		TTL:             pushTTLSeconds,
		Urgency:         webpush.UrgencyHigh,
		HTTPClient:      pushHTTPClient,
	})
	if sendErr != nil {
		return 0, sendErr
	}
	defer response.Body.Close()
	return response.StatusCode, nil
}

// pushUserMayRead : sans comptes tout home est permis ; avec comptes, le droit est relu à chaque envoi.
func pushUserMayRead(userID int64, homeID string) bool {
	accountStore, accountsOn, stateErr := authStateFn()
	if stateErr != nil {
		return false
	}
	if !accountsOn {
		return true
	}
	grantedRights, rightsErr := accountStore.Rights(userID, homeID)
	return rightsErr == nil && grantedRights[RightSessionsRead]
}

func pushWantsEvent(subscription pushSubscription, kind string) bool {
	if kind == "asking" {
		return subscription.Asking
	}
	return subscription.Waiting
}

func pushPayloadFor(subscription pushSubscription, event pushEvent) []byte {
	body := event.Title
	if body == "" {
		body = event.SessionID
	}
	encoded, _ := json.Marshal(pushPayload{
		Title: tr(subscription.Lang, "srv.push."+event.Kind, nil),
		Body:  body,
		Tag:   event.SessionID,
		URL:   "/?s=" + url.PathEscape(event.SessionID),
	})
	return encoded
}

const (
	statusGone     = 410
	statusNotFound = 404
)

func pushDispatch(event pushEvent) {
	subscriptions, loadErr := pushSubscriptionsLoad()
	if loadErr != nil {
		journal("push: abonnements illisibles : %v", loadErr)
		return
	}
	for _, subscription := range subscriptions {
		if !pushWantsEvent(subscription, event.Kind) || !pushMayRead(subscription.UserID, event.HomeID) {
			continue
		}
		statusCode, sendErr := pushTransport(subscription, pushPayloadFor(subscription, event))
		if sendErr != nil {
			journal("push: envoi échoué : %v", sendErr)
			continue
		}
		if statusCode == statusGone || statusCode == statusNotFound {
			_ = pushSubscriptionDelete(subscription.Endpoint)
		}
	}
}
