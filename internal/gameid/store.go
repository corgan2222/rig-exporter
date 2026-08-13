package gameid

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// storeEndpoint is Steam's public store search. No key, no account, no cookie —
// the same request the search box on the store page makes.
//
// The parameters are fixed rather than taken from the configured language:
// l=en so the title comes back spelled the way the store's own catalogue spells
// it, cc=DE because the endpoint insists on a country and the app id does not
// depend on which one. Prices come back too and are thrown away here.
const storeEndpoint = "https://store.steampowered.com/api/storesearch/"

// storeTimeout is how long one lookup may take. It runs off the measurement
// goroutine, so this bounds a request rather than a reading.
const storeTimeout = 10 * time.Second

// SteamStore asks Steam's store about a term.
//
// This is the one function in this program that talks to a third party. What
// leaves the machine is a game's title and nothing else — no identifier, no
// hardware, no configuration — and what comes back is an app id and the title
// the store keeps for it. It runs only for a game Steam itself did not name,
// and only once per term.
//
// The title matters as much as the id when the term was worked out from a file
// name: "Cyberpunk 2077" is what the store calls what the machine calls
// Cyberpunk2077.exe, and the store's spelling is a fact where the term was a
// guess.
func SteamStore(term string) (Match, bool) {
	endpoint := storeEndpoint + "?l=en&cc=DE&term=" + url.QueryEscape(term)

	client := &http.Client{Timeout: storeTimeout}
	resp, err := client.Get(endpoint)
	if err != nil {
		return Match{}, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Match{}, false
	}

	var body struct {
		Items []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
	}
	if json.NewDecoder(resp.Body).Decode(&body) != nil || len(body.Items) == 0 {
		return Match{}, false
	}

	// The first item, which is what the store considers the best match for the
	// term. Measured: "Cyberpunk 2077" answers 1091500 and "DOOM 64" 1148590.
	// An id of zero is not an answer.
	if body.Items[0].ID <= 0 {
		return Match{}, false
	}
	return Match{
		AppID: strconv.Itoa(body.Items[0].ID),
		Title: strings.TrimSpace(body.Items[0].Name),
	}, true
}
