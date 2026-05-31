package collector

import (
	"encoding/json"
	"fmt"

	"github.com/homeylab/tdarr-exporter/internal/client"
)

// fakeKey identifies a logical Tdarr request so the fake can route to a fixture
// and/or inject an error. For cruddb POSTs the discriminator is the payload's
// collection field; for get-pies POSTs it is the libraryId; for GET requests it
// is the path itself.
type fakeKey struct {
	path string
	// disc is the secondary discriminator: the cruddb collection name
	// (StatisticsJSONDB / LibrarySettingsJSONDB) or the get-pies libraryId.
	// Empty for plain GET requests keyed only on path.
	disc string
}

// fakeTdarrAPI is an in-memory tdarrAPI implementation backed by raw JSON fixture
// bytes. It mimics the real client by json.Unmarshal-ing the fixture into target.
// It replaces the real client + httptest server in collector tests, removing the
// multi-second retry/backoff incurred when a real client hits a failing endpoint.
type fakeTdarrAPI struct {
	// responses maps a request key to the raw JSON fixture returned for it.
	responses map[fakeKey][]byte
	// errors maps a request key to an error that should be returned instead of a
	// response, used to drive tdarr_up=0 / partial-failure paths.
	errors map[fakeKey]error
}

// statErr is returned by the fake to mimic a non-2xx / transport failure from the
// real client without any network or backoff.
type statErr struct{ msg string }

func (e statErr) Error() string { return e.msg }

func newFakeTdarrAPI() *fakeTdarrAPI {
	return &fakeTdarrAPI{
		responses: make(map[fakeKey][]byte),
		errors:    make(map[fakeKey]error),
	}
}

// setResponse registers fixture bytes for a request key.
func (f *fakeTdarrAPI) setResponse(key fakeKey, body []byte) {
	f.responses[key] = body
}

// setError registers an error for a request key; it takes precedence over any
// configured response for that key.
func (f *fakeTdarrAPI) setError(key fakeKey, err error) {
	f.errors[key] = err
}

// keyForPost derives the routing key for a DoPostRequest payload. It inspects the
// JSON payload to extract the cruddb collection name or the get-pies libraryId.
func keyForPost(path string, payload []byte) (fakeKey, error) {
	// Try cruddb shape first (has data.collection).
	var cruddb TdarrMetricRequest
	if err := json.Unmarshal(payload, &cruddb); err == nil && cruddb.Data.Collection != "" {
		return fakeKey{path: path, disc: cruddb.Data.Collection}, nil
	}
	// Fall back to get-pies shape (has data.libraryId).
	var pie TdarrPieDataRequest
	if err := json.Unmarshal(payload, &pie); err == nil {
		return fakeKey{path: path, disc: pie.Data.LibraryId}, nil
	}
	return fakeKey{}, fmt.Errorf("fakeTdarrAPI: could not derive key for POST %s payload %s", path, payload)
}

func (f *fakeTdarrAPI) DoPostRequest(path string, target interface{}, payload []byte) error {
	key, err := keyForPost(path, payload)
	if err != nil {
		return err
	}
	if e, ok := f.errors[key]; ok {
		return e
	}
	body, ok := f.responses[key]
	if !ok {
		return fmt.Errorf("fakeTdarrAPI: no response registered for POST %s disc=%q", key.path, key.disc)
	}
	return json.Unmarshal(body, target)
}

func (f *fakeTdarrAPI) DoRequest(path string, target interface{}, queryParams ...client.QueryParams) error {
	key := fakeKey{path: path}
	if e, ok := f.errors[key]; ok {
		return e
	}
	body, ok := f.responses[key]
	if !ok {
		return fmt.Errorf("fakeTdarrAPI: no response registered for GET %s", path)
	}
	return json.Unmarshal(body, target)
}
