package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"regexp"
	"strings"
)

var hostnameLabel = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

func subscriptionAtDomain(address, domain string) string {
	domain = strings.TrimSpace(domain)
	if len(domain) > 253 || !strings.Contains(domain, ".") || net.ParseIP(domain) != nil {
		return ""
	}
	for _, label := range strings.Split(domain, ".") {
		if !hostnameLabel.MatchString(label) {
			return ""
		}
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.Host == "" || strings.EqualFold(parsed.Hostname(), domain) {
		return ""
	}
	if port := parsed.Port(); port != "" {
		domain = net.JoinHostPort(domain, port)
	}
	parsed.Host = domain
	return parsed.String()
}

func recoveryDomain(address string) (string, error) {
	response, err := doSubscriptionRequest(address, "", false)
	if err != nil {
		return "", err
	}
	var manifest struct {
		Domain string `json:"domain"`
	}
	if err := json.Unmarshal([]byte(response.body), &manifest); err != nil || manifest.Domain == "" {
		return "", errors.New("файл восстановления должен содержать поле domain")
	}
	return manifest.Domain, nil
}

func managedSubscriptionProfile(settings managedSubscription, stateDir string) (managedProfile, string, error) {
	load := func(address string) (managedProfile, subscriptionResponse, error) {
		response, err := fetchSubscriptionResponse(subscription{tag: managedTag, url: address}, stateDir)
		if err != nil {
			return managedProfile{}, response, err
		}
		profile, err := compileManagedSubscription(response.body)
		profile.probeURL = response.header.Get("check-url-via-proxy")
		return profile, response, err
	}
	address := settings.URL
	profile, response, err := load(address)
	if errors.Is(err, errSubscriptionUnavailable) && settings.RecoveryURL != "" {
		domain, recoveryErr := recoveryDomain(settings.RecoveryURL)
		if recoveryErr != nil {
			return managedProfile{}, "", fmt.Errorf("не удалось прочитать файл восстановления подписки: %w", recoveryErr)
		}
		address = subscriptionAtDomain(address, domain)
		if address == "" {
			return managedProfile{}, "", errors.New("файл восстановления не указал другой допустимый домен")
		}
		profile, response, err = load(address)
	}
	if err != nil {
		return managedProfile{}, "", err
	}
	if target := subscriptionAtDomain(address, response.header.Get("new-domain")); target != "" {
		if migrated, _, err := load(target); err == nil {
			return migrated, target, nil
		}
		log.Print("Новый домен подписки пока не готов. Используется рабочий адрес.")
	}
	return profile, address, nil
}
