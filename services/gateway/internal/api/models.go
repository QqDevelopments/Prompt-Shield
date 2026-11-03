package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type providerModel struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Created int64  `json:"-"`
}

// Model discovery uses only the caller's credential and configured provider URLs.
// Neither credentials nor account model lists are cached or persisted.
func (s *Server) models(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Provider string `json:"provider"`
	}
	if decode(w, r, &body) != nil || !validProvider(body.Provider) {
		fail(w, 400, "invalid_request")
		return
	}
	keys := r.Header.Values("X-Provider-API-Key")
	if len(keys) == 0 {
		fail(w, 401, "provider_key_required")
		return
	}
	if len(keys) != 1 || !validProviderKey(keys[0]) {
		fail(w, 400, "invalid_provider_key")
		return
	}
	base, _ := s.providerConfiguration(body.Provider)
	endpoint, err := url.Parse(strings.TrimRight(base, "/") + "/models")
	if err != nil {
		fail(w, 502, "models_unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	models := []providerModel{}
	seen := map[string]bool{}
	cursors := map[string]bool{}
	remaining := int64(2 * 1024 * 1024)
	buffer := buffers.Get().(*bytes.Buffer)
	defer release(buffer)
	for page := 0; page < 10; page++ {
		if body.Provider == "anthropic" {
			query := endpoint.Query()
			query.Set("limit", "1000")
			endpoint.RawQuery = query.Encode()
		}
		upstream, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		if err != nil {
			fail(w, 502, "models_unavailable")
			return
		}
		if body.Provider == "anthropic" {
			upstream.Header.Set("x-api-key", keys[0])
			upstream.Header.Set("anthropic-version", "2023-06-01")
		} else {
			upstream.Header.Set("Authorization", "Bearer "+keys[0])
		}
		response, err := s.client.Do(upstream)
		if err != nil {
			fail(w, 502, "models_unavailable")
			return
		}
		if response.StatusCode != http.StatusOK {
			rejected := providerKeyRejected(body.Provider, response)
			response.Body.Close()
			if rejected {
				fail(w, 401, "provider_key_rejected")
				return
			}
			switch response.StatusCode {
			case 401:
				fail(w, 401, "provider_key_rejected")
			case 403:
				fail(w, 403, "models_forbidden")
			case 429:
				fail(w, 429, "rate_limited")
			default:
				fail(w, 502, "models_unavailable")
			}
			return
		}
		buffer.Reset()
		n, readErr := io.Copy(buffer, io.LimitReader(response.Body, remaining+1))
		response.Body.Close()
		remaining -= n
		var catalog struct {
			Data []struct {
				ID          string `json:"id"`
				DisplayName string `json:"display_name"`
				Created     int64  `json:"created"`
				CreatedAt   string `json:"created_at"`
			} `json:"data"`
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
		}
		if readErr != nil || remaining < 0 || json.Unmarshal(buffer.Bytes(), &catalog) != nil || catalog.Data == nil {
			fail(w, 502, "models_unavailable")
			return
		}
		for _, model := range catalog.Data {
			if !validModelID(model.ID) || seen[model.ID] {
				continue
			}
			seen[model.ID] = true
			name := model.DisplayName
			if name == "" || len(name) > 200 {
				name = model.ID
			}
			created := model.Created
			if date, err := time.Parse(time.RFC3339, model.CreatedAt); err == nil {
				created = date.Unix()
			}
			models = append(models, providerModel{ID: model.ID, Name: name, Created: created})
		}
		if len(models) > 2000 {
			fail(w, 502, "models_unavailable")
			return
		}
		if !catalog.HasMore {
			sort.Slice(models, func(i, j int) bool {
				if models[i].Created != models[j].Created {
					return models[i].Created > models[j].Created
				}
				return models[i].ID < models[j].ID
			})
			respond(w, 200, map[string]any{"data": models})
			return
		}
		if body.Provider != "anthropic" || !validModelID(catalog.LastID) || cursors[catalog.LastID] {
			fail(w, 502, "models_unavailable")
			return
		}
		cursors[catalog.LastID] = true
		query := endpoint.Query()
		query.Set("after_id", catalog.LastID)
		endpoint.RawQuery = query.Encode()
	}
	fail(w, 502, "models_unavailable")
}

func validModelID(id string) bool {
	if id == "" || len(id) > 150 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._-:/", c)) {
			return false
		}
	}
	return true
}
