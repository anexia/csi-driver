package server

import "strings"

// parseEndpoint parses a given endpoint into protocol and address for the given protocol.
func parseEndpoint(endpoint string) (string, string, error) {
	epLower := strings.ToLower(endpoint)
	if strings.HasPrefix(epLower, "unix://") || strings.HasPrefix(epLower, "tcp://") {
		s := strings.SplitN(endpoint, "://", 2)

		if s[1] != "" {
			return s[0], s[1], nil
		}
	}
	return "", "", ErrInvalidEndpoint
}
