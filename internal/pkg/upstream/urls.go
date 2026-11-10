package upstream

import (
	"errors"
	"net/url"
	"strings"
)

func ParseBase(base string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("上游地址须为不含凭据和查询参数的 HTTP(S) Base URL")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u, nil
}

// ChatURL accepts a host root or an API prefix, including non-v1 vendor APIs.
func ChatURL(base string) (string, error) {
	u, err := ParseBase(base)
	if err != nil {
		return "", err
	}
	if strings.HasSuffix(u.Path, "/chat/completions") {
		return u.String(), nil
	}
	if u.Path == "" {
		u.Path = "/v1"
	}
	u.Path += "/chat/completions"
	return u.String(), nil
}

func ModelsURL(base string) (string, error) {
	u, err := ParseBase(base)
	if err != nil {
		return "", err
	}
	u.Path = strings.TrimSuffix(u.Path, "/chat/completions")
	if strings.HasSuffix(u.Path, "/models") {
		return u.String(), nil
	}
	if u.Path == "" {
		u.Path = "/v1"
	}
	u.Path += "/models"
	return u.String(), nil
}

// NativeURL preserves vendor prefixes and supports explicit protocol base URLs.
func NativeURL(base, override, kind string) (string, error) {
	if override != "" {
		u, err := ParseBase(override)
		if err != nil {
			return "", err
		}
		if kind == "messages" {
			if !strings.HasSuffix(u.Path, "/messages") {
				if !strings.HasSuffix(u.Path, "/v1") {
					u.Path += "/v1"
				}
				u.Path += "/messages"
			}
		} else if kind == "responses" {
			if !strings.HasSuffix(u.Path, "/responses") {
				u.Path += "/responses"
			}
		} else {
			return "", errors.New("unsupported upstream protocol")
		}
		return u.String(), nil
	}
	u, err := ParseBase(base)
	if err != nil {
		return "", err
	}
	u.Path = strings.TrimSuffix(u.Path, "/chat/completions")
	if kind == "messages" {
		switch {
		case strings.HasSuffix(u.Path, "/messages"):
		case u.Path == "/api/coding/paas/v4" || u.Path == "/api/paas/v4":
			u.Path = "/api/anthropic/v1/messages"
		case strings.HasSuffix(u.Path, "/anthropic/v1"):
			u.Path += "/messages"
		case strings.HasSuffix(u.Path, "/anthropic"):
			u.Path += "/v1/messages"
		case (u.Path == "" || u.Path == "/v1") && u.Hostname() == "api.deepseek.com":
			u.Path = "/anthropic/v1/messages"
		case u.Path == "/v1" || u.Hostname() == "api.anthropic.com" && u.Path == "":
			u.Path = "/v1/messages"
		case u.Path == "":
			u.Path = "/anthropic/v1/messages"
		case strings.HasSuffix(u.Path, "/v1"):
			u.Path += "/messages"
		default:
			u.Path += "/v1/messages"
		}
	} else if kind == "responses" {
		if strings.HasSuffix(u.Path, "/responses") {
			return u.String(), nil
		}
		if u.Path == "/v1" && u.Hostname() == "api.deepseek.com" {
			u.Path = ""
		}
		if u.Path == "" && u.Hostname() == "api.openai.com" {
			u.Path = "/v1"
		}
		u.Path += "/responses"
	} else {
		return "", errors.New("unsupported upstream protocol")
	}
	return u.String(), nil
}
