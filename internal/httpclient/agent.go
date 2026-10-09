package httpclient

import "net/http"

// UserAgent is sent with HTTP requests made by the CLI.
const UserAgent = "createos-cli"

// SetUserAgent identifies HTTP requests made by the CLI.
func SetUserAgent(req *http.Request) {
	req.Header.Set("User-Agent", UserAgent)
}
