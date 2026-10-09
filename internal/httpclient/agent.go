package httpclient

import "net/http"

const UserAgent = "createos-cli"

// SetUserAgent identifies HTTP requests made by the CLI.
func SetUserAgent(req *http.Request) {
	req.Header.Set("User-Agent", UserAgent)
}
