package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"golang.org/x/net/publicsuffix"
)

var errRuntimeCookieDomain = errors.New("invalid shared runtime cookie domain")

// RuntimeCookieDomain is the common, controlled domain of the application and runtime hosts.
func RuntimeCookieDomain(application *url.URL, template string) (string, error) {
	if application == nil {
		return "", fmt.Errorf("%w: application origin required", errRuntimeCookieDomain)
	}
	host := application.Hostname()
	if template == "" {
		return host, nil
	}
	runtime, err := url.Parse(strings.Replace(template, "{launchId}", "runtime", 1))
	if err != nil || runtime.Scheme != application.Scheme {
		return "", fmt.Errorf("%w: matching schemes required", errRuntimeCookieDomain)
	}
	if net.ParseIP(host) != nil {
		return "", fmt.Errorf("%w: DNS hosts required", errRuntimeCookieDomain)
	}
	for domain := host; domain != ""; {
		if runtime.Hostname() == domain || strings.HasSuffix(runtime.Hostname(), "."+domain) {
			suffix, _ := publicsuffix.PublicSuffix(domain)
			if domain == "localhost" || suffix != domain {
				return domain, nil
			}
			break
		}
		_, next, ok := strings.Cut(domain, ".")
		if !ok {
			break
		}
		domain = next
	}
	return "", fmt.Errorf("%w: application and runtime require a shared non-public parent", errRuntimeCookieDomain)
}
